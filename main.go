package main

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"net"

	"golang.org/x/crypto/ssh"
)

// PrivKeyLocation is the location of the private key to be used
// in the ssh server
const PrivKeyLocation string = "/Users/cernec1999/.ssh/id_rsa"

// RemoteUsername is the username of the remote server
// const RemoteUsername string = "dev"
const RemoteUsername string = "ssmp"

// RemotePassword is the remote's password
// const RemotePassword string = "j.#dM#N<`w>Ehv8:7\"4X8cpy\"f)2X5"
const RemotePassword string = "&qWKKa$Lb*okfwtzhm8fGa2H&"

// RemoteAddr describes the remote server to connect to
// const RemoteAddr string = "127.0.0.1:1234"
const RemoteAddr string = "dadb0d.commentblock.com:22"

// ServerAddr is the address and port to bind to
const ServerAddr string = ":22"

// Dbg is if we are in debug mode
const Dbg bool = true

// PasswordData represents metadata about the password
type PasswordData struct {
	lastAttemptedPassword string
	attempts              uint8
}

// Creates a connection to the remote SSH server
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

// Serve a single SSH connection
func serveSSHConnection(connection net.Conn, sshConfig *ssh.ServerConfig, passwords map[net.Addr]PasswordData) error {
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

	// Get the password data for that connection
	pwdData := passwords[serverConnection.Conn.RemoteAddr()]

	// Create SQL connection
	sqlConn := NewSQLHoneypotDBConnection(serverConnection.Conn.RemoteAddr().String(), "unk", serverConnection.Conn.User(), pwdData.lastAttemptedPassword, pwdData.attempts)

	// Remove old password data
	delete(passwords, serverConnection.Conn.RemoteAddr())

	// Close client connection on exit
	defer clientConnection.Close()
	defer sqlConn.Close()

	go ssh.DiscardRequests(serverRequests)

	// Iterate through all the channels (is there just one?)
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
				var req *ssh.Request = nil
				var dst ssh.Channel = nil

				select {
				case req = <-serverRequests:
					dst = clientChannel
				case req = <-clientRequests:
					dst = serverChannel
				}

				// Resolve segmentation fault for unexpected closed connection
				if dst == nil || req == nil {
					debugPrint("Client closed connection unexpectedly")
					return
				}

				b, err := dst.SendRequest(req.Type, req.WantReply, req.Payload)
				if err != nil {
					debugPrint(fmt.Sprintf("Request sending did not work %s", err))
					return
				}

				if req.WantReply {
					req.Reply(b, nil)
				}

				if req.Type == "exit-status" {
					break threadLoop
				}
			}

			// Finally, close the connections
			serverChannel.Close()
			clientChannel.Close()
		}()

		var wrappedServerChannel io.ReadCloser = serverChannel
		var wrappedClientChannel io.ReadCloser = NewSQLReadCloser(clientChannel, sqlConn)

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
	passwords := make(map[net.Addr]PasswordData)

	// Configure ssh server
	config := &ssh.ServerConfig{
		PasswordCallback: func(connMeta ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {

			debugPrint(fmt.Sprintf("SSH password attempt from %s.", connMeta.RemoteAddr()))
			debugPrint(fmt.Sprintf("Username: %s", connMeta.User()))
			debugPrint(fmt.Sprintf("Password: %s", string(password)))

			// See if we let them in
			succeed := rand.Intn(3) == 0

			// If we've seen this connection before
			if _, ok := passwords[connMeta.RemoteAddr()]; !ok {
				passwords[connMeta.RemoteAddr()] = PasswordData{
					lastAttemptedPassword: string(password),
					attempts:              1,
				}

				if succeed {
					return nil, nil
				}
			} else if passwords[connMeta.RemoteAddr()].attempts == 3 {
				passwords[connMeta.RemoteAddr()] = PasswordData{
					lastAttemptedPassword: string(password),
					attempts:              3,
				}
				return nil, nil
			} else {
				passwords[connMeta.RemoteAddr()] = PasswordData{
					lastAttemptedPassword: string(password),
					attempts:              passwords[connMeta.RemoteAddr()].attempts + 1,
				}

				if succeed {
					return nil, nil
				}
			}

			return nil, errors.New("Incorrect SSH password")
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
			debugPrint(fmt.Sprintf("listener.Accept failed: %v", err))
			continue
		}

		go serveSSHConnection(currentConnection, config, passwords)
	}
}

func debugPrint(str string) {
	if Dbg {
		fmt.Printf("[DBG] %s\n", str)
	}
}
