package main

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"

	"golang.org/x/crypto/ssh"
)

// NewSQLReadCloser creates a new SqlReadCloser struct
func NewSQLReadCloser(r io.ReadCloser) io.ReadCloser {
	return &SQLReadCloser{ReadCloser: r}
}

// SQLReadCloser type to export into a DB
type SQLReadCloser struct {
	io.ReadCloser
	buffer bytes.Buffer
}

func (sq *SQLReadCloser) Read(p []byte) (n int, err error) {
	n, err = sq.ReadCloser.Read(p)
	sq.buffer.Write(p)
	return n, err
}

func (sq *SQLReadCloser) String() string {
	return sq.buffer.String()
}

// Close the connection
func (sq *SQLReadCloser) Close() error {
	fmt.Println(sq.buffer.String())
	return sq.ReadCloser.Close()
}

// PrivKeyLocation is the location of the private key to be used
// in the ssh server
const PrivKeyLocation string = "/Users/cernec1999/.ssh/id_rsa"

// RemoteUsername is the username of the remote server
//const RemoteUsername string = "dev"
const RemoteUsername string = "ssmp"

// RemotePassword is the remote's password
//const RemotePassword string = "j.#dM#N<`w>Ehv8:7\"4X8cpy\"f)2X5"
const RemotePassword string = "&qWKKa$Lb*okfwtzhm8fGa2H&"

const RemoteAddr string = "dadb0d.commentblock.com:22"

// ServerAddr is the address and port to bind to
const ServerAddr string = ":1337"

// Dbg is if we are in debug mode
const Dbg bool = true

func dialSSHClient() (*ssh.Client, error) {
	// Configure an ssh client
	clientConfig := &ssh.ClientConfig{}

	clientConfig.User = RemoteUsername
	clientConfig.Auth = []ssh.AuthMethod{
		ssh.Password(RemotePassword),
	}

	// Ignore host key verification
	clientConfig.HostKeyCallback = ssh.InsecureIgnoreHostKey()

	client, err := ssh.Dial("tcp", RemoteAddr, clientConfig)

	return client, err
}

func serveSSHConnection(connection net.Conn, sshConfig *ssh.ServerConfig) error {
	serverConnection, serverChannels, serverRequests, err := ssh.NewServerConn(connection, sshConfig)

	if err != nil {
		debugPrint("Could not initiate SSH handshake")
		return err
	}

	// Close connection when function returns
	defer serverConnection.Close()

	// Proxy the SSH request by dialing a new ssh client
	clientConnection, err := dialSSHClient()
	if err != nil {
		debugPrint("Could not dial SSH client")
		fmt.Printf("%v\n", err)
		return err
	}

	// Close client connection on exit
	defer clientConnection.Close()

	go ssh.DiscardRequests(serverRequests)

	for newChannel := range serverChannels {
		// Create client connection
		clientChannel, clientRequests, err := clientConnection.OpenChannel(newChannel.ChannelType(), newChannel.ExtraData())
		if err != nil {
			log.Fatalf("Could not accept client channel: %s", err.Error())
			return err
		}

		serverChannel, serverRequests, err := newChannel.Accept()
		if err != nil {
			debugPrint(fmt.Sprintf("Could not accept channel: %v", err))
			return err
		}

		// Threads that basically get requests
		go func() {
		threadLoop:
			for {
				var req *ssh.Request
				var dst ssh.Channel

				select {
				case req = <-serverRequests:
					dst = clientChannel
				case req = <-clientRequests:
					dst = serverChannel
				}

				// TODO: Fix segfault here. Nil check for dst?
				b, err := dst.SendRequest(req.Type, req.WantReply, req.Payload)
				if err != nil {
					debugPrint(fmt.Sprintf("Request sending did not work %s", err))
				}

				if req.WantReply {
					req.Reply(b, nil)
				}

				if req.Type == "exit-status" {
					break threadLoop
				}
			}
			serverChannel.Close()
			clientChannel.Close()
		}()

		var wrappedServerChannel io.ReadCloser = serverChannel
		var wrappedClientChannel io.ReadCloser = NewSQLReadCloser(clientChannel)

		/*if p.wrapFn != nil {
			// wrappedChannel, err = p.wrapFn(channel)
			// wrappedClientChannel, err = p.wrapFn(serverConnection, channel2)
		}*/

		go io.Copy(clientChannel, wrappedServerChannel)
		go io.Copy(serverChannel, wrappedClientChannel)

		defer wrappedServerChannel.Close()
		defer wrappedClientChannel.Close()
	}

	return nil
}

func main() {
	// Read private key file
	privateKeyBytes, err := ioutil.ReadFile(PrivKeyLocation)

	// If error, exit
	if err != nil {
		panic("Failed to read private key file.")
	}

	// Turn bytes into a real private key
	privateKey, err := ssh.ParsePrivateKey(privateKeyBytes)

	// If error, exit
	if err != nil {
		panic("Failed to parse private key file.")
	}

	// Create a map for all the clients

	// Configure ssh server
	config := &ssh.ServerConfig{
		PasswordCallback: func(connMeta ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {

			debugPrint(fmt.Sprintf("SSH Connection from %s.", connMeta.RemoteAddr()))
			debugPrint(fmt.Sprintf("Username: %s", connMeta.User()))
			debugPrint(fmt.Sprintf("Password: %s", string(password)))

			return nil, nil
		},
	}

	// Add id_rsa to the ssh config
	config.AddHostKey(privateKey)

	// Now spin up the server
	listener, err := net.Listen("tcp", ServerAddr)

	defer listener.Close()

	if err != nil {
		panic(fmt.Sprintf("net.Listen failed: %v", err))
	}

	// Loop infinitaly
	for {
		currentConnection, err := listener.Accept()

		if err != nil {
			panic(fmt.Sprintf("listener.Accept failed: %v", err))
		}

		serveSSHConnection(currentConnection, config)
	}
}

func debugPrint(str string) {
	if Dbg {
		fmt.Printf("[DBG] %s\n", str)
	}
}
