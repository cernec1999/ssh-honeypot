package main

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"net"
	"os"
	"os/signal"
	"strconv"
	"time"

	"golang.org/x/crypto/ssh"
)

// PrivKeyLocation is the location of the private key to be used
// in the ssh server
const PrivKeyLocation string = "/Users/cernec1999/.ssh/id_rsa"

// RemoteUsername is the username of the remote server
const RemoteUsername string = "root"

// RemotePassword is the remote's password
const RemotePassword string = "root"

// RemoteAddr describes the remote server to connect to
const RemoteAddr string = "127.0.0.1"

// ServerAddr is the address and port to bind to
const ServerAddr string = "0.0.0.0:22"

// Dbg is if we are in debug mode
const Dbg bool = true

// NumContainers to have available
// We don't ever need to spawn more than 255. That's crazy talk.
const NumContainers uint8 = 5

// KeepAliveTime specifies how long to keep the container alive
const KeepAliveTime string = "5m"

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

// global channel of available containers
// Methinks we should make a struct and put a semaphore inside of it
var availableContainers chan string

// Is the program still running?
var programIsRunning bool = true

// Function to ensure that there will always be at least NumContainers
// Note: This will spawn one more container that doesn't get added into
// the channel list. Not sure if there is Golang syntax to fix this, but
// we'll call it a feature, not a bug. I know we can get around this using
// other synchronization constructs like Semaphores, but I like the syntax
// of channels.
func spawnContainers() {
	for programIsRunning {
		str, err := CreateAndStartNewContainer()

		if err != nil {
			debugPrint(fmt.Sprintf("Error starting container: %v", err))
			continue
		}

		// This blocks if n containers are already in the list
		availableContainers <- str
	}

}

// Creates a connection to the remote SSH server
func dialSSHClient() (*ssh.Client, string, error) {
	// Pop new connection
	conn := <-availableContainers

	// Do we have a race condition here where
	// the container could still be running with
	// SIGINT at this point?

	// Get NAT'd port number
	port, err := GetHostPort(conn)

	if err != nil {
		return nil, "", err
	}

	// Configure an ssh client
	clientConfig := &ssh.ClientConfig{}

	clientConfig.User = RemoteUsername
	clientConfig.Auth = []ssh.AuthMethod{
		ssh.Password(RemotePassword),
	}

	// Ignore host key verification
	clientConfig.HostKeyCallback = ssh.InsecureIgnoreHostKey()

	// Finally, redirect to docker container
	client, err := ssh.Dial("tcp", fmt.Sprintf("%s:%s", RemoteAddr, port), clientConfig)

	return client, conn, err
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
	clientConnection, containerID, err := dialSSHClient()
	if err != nil {
		debugPrint(fmt.Sprintf("Could not dial SSH client to %s: %v", containerID, err))
		return err
	}

	// Stop the container after n seconds
	dur, _ := time.ParseDuration(KeepAliveTime)

	timer := time.AfterFunc(dur, func() {
		StopContainer(containerID)
	})
	defer timer.Stop()

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

	// Write debug
	debugPrint(fmt.Sprintf("SSH connection authenticated for %s. Writing to database with ID %d.", host, sqlConn.ConnID))

	// Remove old password data
	delete(passwords, serverConnection.Conn.RemoteAddr())

	// Close client connection on exit
	defer clientConnection.Close()
	defer sqlConn.Close()
	defer func() {
		debugPrint("Closing connection and stopping containers.")
		err = StopContainer(containerID)
		if err != nil {
			debugPrint(fmt.Sprintf("Error closing container: %v", err))
		}
	}()

	go ssh.DiscardRequests(serverRequests)

	// Iterate through all the channels (is there just one?)
	for newChannel := range serverChannels {
		// Create client connection
		clientChannel, clientRequests, err := clientConnection.OpenChannel(newChannel.ChannelType(), newChannel.ExtraData())
		if err != nil {
			debugPrint(fmt.Sprintf("Could not accept client channel: %s", err.Error()))
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

			// We should kill the docker container after x seconds?

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
	// TODO: is there a data race here? need to check if passwords is thread safe.
	passwords := make(map[net.Addr]PasswordAttemptData)

	// Create a channel list for the new containers
	availableContainers = make(chan string, NumContainers)

	// Create the initial containers
	go spawnContainers()

	// SigINT handling
	// This cleanly stops the docker containers
	c := make(chan os.Signal)
	signal.Notify(c, os.Interrupt)
	go func() {
		for range c {
			programIsRunning = false

			// Pull connections from the channel and kill them. At
			// this point, we shouldn't have any more additions, only
			// removals.
		innerLoop:
			for {
				select {
				case cont := <-availableContainers:
					StopContainer(cont)
				default:
					break innerLoop
				}
			}

			// TODO: Clean up actively running connections.

			debugPrint("Exiting honeypot")

			// Exit
			os.Exit(0)
		}
	}()

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
