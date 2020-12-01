package main

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"net"
	"strconv"

	"golang.org/x/crypto/ssh"
)

// PrivKeyLocation is the location of the private key to be used
// in the ssh server
const PrivKeyLocation string = "/Users/cernec1999/.ssh/id_rsa"

// RemoteUsername is the username of the remote server
const RemoteUsername string = "dev"

// RemotePassword is the remote's password
const RemotePassword string = "j.#dM#N<`w>Ehv8:7\"4X8cpy\"f)2X5"

// RemoteAddr describes the remote server to connect to
const RemoteAddr string = "127.0.0.1:1234"

// ServerAddr is the address and port to bind to
const ServerAddr string = "0.0.0.0:22"

// Dbg is if we are in debug mode
const Dbg bool = true

// PasswordAttemptData represents metadata about password attempts
type PasswordAttemptData struct {
	usernamePasswords []UsernamePassword
	numAttempts       uint8
}

// UsernamePassword has a specific attempt information
type UsernamePassword struct {
	username string
	password string
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
func serveSSHConnection(connection net.Conn, sshConfig *ssh.ServerConfig, passwords map[net.Addr]PasswordAttemptData) error {
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
		debugPrint(fmt.Sprintf("Could not dial SSH client: %v", err))
		return err
	}

	// Get the password data for that connection
	pwdData := passwords[serverConnection.Conn.RemoteAddr()]

	// Split address
	host, strPort, err := net.SplitHostPort(serverConnection.Conn.RemoteAddr().String())

	if err != nil {
		debugPrint(fmt.Sprintf("Could not split host and port: %v", err))
		return err
	}

	port, err := strconv.ParseUint(strPort, 10, 16)

	if err != nil {
		debugPrint(fmt.Sprintf("Could not parse port as an integer: %v", err))
		return err
	}

	geoData := GetGeoData(host)

	// Create SQL connection
	sqlConn := NewSQLHoneypotDBConnection(host, uint16(port), geoData, pwdData)

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
	passwords := make(map[net.Addr]PasswordAttemptData)

	// Configure ssh server
	config := &ssh.ServerConfig{
		PasswordCallback: func(connMeta ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {

			debugPrint(fmt.Sprintf("SSH password attempt from %s.", connMeta.RemoteAddr()))
			debugPrint(fmt.Sprintf("Username: %s", connMeta.User()))
			debugPrint(fmt.Sprintf("Password: %s", string(password)))

			// See if we let them in
			succeed := rand.Intn(3) == 0

			// If we've not seen this connection before
			if _, ok := passwords[connMeta.RemoteAddr()]; !ok {
				attemptsList := []UsernamePassword{}

				attemptsList = append(attemptsList, UsernamePassword{
					username: connMeta.User(),
					password: string(password),
				})

				passwords[connMeta.RemoteAddr()] = PasswordAttemptData{
					usernamePasswords: attemptsList,
					numAttempts:       1,
				}

				if succeed {
					return nil, nil
				}
			} else if passwords[connMeta.RemoteAddr()].numAttempts == 2 {
				// Get password list and append to it
				pwdList := passwords[connMeta.RemoteAddr()].usernamePasswords
				pwdList = append(pwdList, UsernamePassword{
					username: connMeta.User(),
					password: string(password),
				})

				// append password attempt
				passwords[connMeta.RemoteAddr()] = PasswordAttemptData{
					usernamePasswords: pwdList,
					numAttempts:       3,
				}

				// Success
				return nil, nil
			} else {
				// Get password list and append to it
				pwdList := passwords[connMeta.RemoteAddr()].usernamePasswords
				pwdList = append(pwdList, UsernamePassword{
					username: connMeta.User(),
					password: string(password),
				})

				// append password attempt
				passwords[connMeta.RemoteAddr()] = PasswordAttemptData{
					usernamePasswords: pwdList,
					numAttempts:       passwords[connMeta.RemoteAddr()].numAttempts + 1,
				}

				// we succeed
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
