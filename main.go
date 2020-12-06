package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"net"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/sync/semaphore"
)

// PrivKeyLocation is the location of the private key to be used
// in the ssh server
const PrivKeyLocation string = "/home/ccerne/.ssh/id_rsa"

// RemoteUsername is the username of the remote server
const RemoteUsername string = "root"

// RemotePassword is the remote's password
const RemotePassword string = "root"

// RemotePort is the remote's port
const RemotePort uint16 = 22

// ServerAddr is the address and port to bind to
const ServerAddr string = "0.0.0.0:1337"

// Dbg is if we are in debug mode
const Dbg bool = true

// NumContainers to have available
// We don't ever need to spawn more than 255. That's crazy talk.
const NumContainers uint8 = 1

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
var availableContainers AvailableContainers

// AvailableContainers describes a list of the container names to be
// popped from.
type AvailableContainers struct {
	containerEvent *semaphore.Weighted
	containerNames []string
}

// Create mutex for the shared progrma data
var sharedProgramData sync.Mutex = sync.Mutex{}

// Is the program still running?
var programIsRunning bool = true

// Current password attempt data
var passwordData map[net.Addr]PasswordAttemptData

// Function to ensure that there will always be at least NumContainers
func spawnContainers() {
	sharedProgramData.Lock()
	for programIsRunning {
		sharedProgramData.Unlock()
		// Acquire a semaphore so that only the max number of containers at
		// any given time is only AvailableContainers
		availableContainers.containerEvent.Acquire(context.Background(), 1)

		// Spawn container
		str, err := CreateAndStartNewContainer()

		if err != nil {
			debugPrint(fmt.Sprintf("Error starting container: %v", err))
			sharedProgramData.Lock()
			continue
		}

		// Lock the list and append the container name
		sharedProgramData.Lock()
		availableContainers.containerNames = append(availableContainers.containerNames, str)
	}

	sharedProgramData.Unlock()

}

// Creates a connection to the remote SSH server
func dialSSHClient(containerID string) (*ssh.Client, string, error) {
	startExistingContainer := false

	if containerID != "" {
		err := StartExistingContainer(containerID)

		if err != nil {
			debugPrint(fmt.Sprintf("Could not start existing container. Will start new: %v", err))
			startExistingContainer = false
		} else {
			startExistingContainer = true
		}
	}

	conn := ""

	if startExistingContainer {
		// Busy wait til the server is up
		for {
			// Busy wait til we're back in business
			bool, err := IsSSHRunning(containerID)

			if err != nil {
				return nil, "", err
			}

			if bool {
				break
			}
		}

		conn = containerID
	} else {
		// Pop new connection, we know these are running
		// TODO: We need to wait for a new container to come
		// if the length of the array is 0 (data race)
		sharedProgramData.Lock()
		if len(availableContainers.containerNames) != 0 {
			conn = availableContainers.containerNames[0]
			availableContainers.containerNames = availableContainers.containerNames[1:]
			availableContainers.containerEvent.Release(1)
		} else {
			return nil, "", errors.New("No container ready to be popped just yet")
		}
		sharedProgramData.Unlock()
	}

	// Get container IP
	ip, err := GetContainerIP(conn)

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
	client, err := ssh.Dial("tcp", fmt.Sprintf("%s:%d", ip, RemotePort), clientConfig)

	return client, conn, err
}

// Serve a single SSH connection
func serveSSHConnection(connection net.Conn, sshConfig *ssh.ServerConfig) error {
	serverConnection, serverChannels, serverRequests, err := ssh.NewServerConn(connection, sshConfig)

	if err != nil {
		debugPrint(fmt.Sprintf("Could not initiate SSH handshake: %v", err))
		return err
	}

	// Close connection when function returns
	defer serverConnection.Close()

	// Split address
	host, strPort, err := net.SplitHostPort(serverConnection.Conn.RemoteAddr().String())

	if err != nil {
		debugPrint(fmt.Sprintf("Could not split host and port: %v", err))
		return err
	}

	// See if connection already exists
	existsContID, err := GetContainerIDFromConnection(host)

	if err != nil {
		debugPrint(fmt.Sprintf("Could not get container ID: %v", err))
		return err
	}

	// Proxy the SSH request by dialing a new ssh client
	clientConnection, containerID, err := dialSSHClient(existsContID)
	if err != nil {
		debugPrint(fmt.Sprintf("Could not dial SSH client to %s: %v", containerID, err))
		return err
	}

	// Stop the container after n seconds
	// TODO: Check to see if the duration is valid before spawning any connection threads
	dur, _ := time.ParseDuration(KeepAliveTime)

	timer := time.AfterFunc(dur, func() {
		StopContainer(containerID)
	})
	defer timer.Stop()

	sharedProgramData.Lock()
	// Get the password data for that connection
	pwdData := passwordData[serverConnection.Conn.RemoteAddr()]

	// Remove old password data
	delete(passwordData, serverConnection.Conn.RemoteAddr())
	sharedProgramData.Unlock()

	port, err := strconv.ParseUint(strPort, 10, 16)

	if err != nil {
		debugPrint(fmt.Sprintf("Could not parse port as an integer: %v", err))
		return err
	}

	geoData := GetGeoData(host)

	// Create SQL connection
	sqlConn := NewSQLHoneypotDBConnection(host, uint16(port), geoData, pwdData, containerID)

	// Write debug
	debugPrint(fmt.Sprintf("SSH connection authenticated for %s. Writing to database with ID %d.", host, sqlConn.ConnID))

	// Close client connection on exit
	defer clientConnection.Close()
	defer sqlConn.Close()
	defer func() {
		debugPrint("Closing connection and stopping container.")
		// TODO: One IP address can be connected in multiple SSH clients
		// We should only stop the container if all of them disconnect.
		// Dunno how to check this now, we need to maintain state better
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

				// TODO: Implement req.Type exec, pty-req, etc.
				// exec would be pretty important for logging

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
	// Also probably want to find a better way to do this. Maybe create a list of
	// activate connections here.
	passwordData = make(map[net.Addr]PasswordAttemptData)

	// Create a channel list for the new containers
	// availableContainers = make(chan string, NumContainers)
	availableContainers = AvailableContainers{
		containerEvent: semaphore.NewWeighted(int64(NumContainers)),
		containerNames: []string{},
	}

	// Create the initial containers
	go spawnContainers()

	// SigINT handling
	// This cleanly stops the docker containers
	c := make(chan os.Signal)
	signal.Notify(c, os.Interrupt)
	go func() {
		for range c {
			sharedProgramData.Lock()
			programIsRunning = false
			sharedProgramData.Unlock()

			// Pull connections from the channel and kill them. At
			// this point, we shouldn't have any more additions, only
			// removals.
		innerLoop:
			for {
				sharedProgramData.Lock()

				// Get container to remove and stop it
				conn := availableContainers.containerNames[0]
				availableContainers.containerNames = availableContainers.containerNames[1:]
				StopContainer(conn)

				// If we popped the last one off, break
				if len(availableContainers.containerNames) == 0 {
					sharedProgramData.Unlock()
					break innerLoop
				}

				// Unlock the mutex
				sharedProgramData.Unlock()

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
			// Lock password data
			sharedProgramData.Lock()

			// Unlock when the program returns
			defer sharedProgramData.Unlock()

			debugPrint(fmt.Sprintf("SSH password attempt from %s.", connMeta.RemoteAddr()))
			debugPrint(fmt.Sprintf("Username: %s", connMeta.User()))
			debugPrint(fmt.Sprintf("Password: %s", string(password)))

			// See if we let them in
			succeed := rand.Intn(3) == 0

			// If we've not seen this connection before
			if _, ok := passwordData[connMeta.RemoteAddr()]; !ok {
				attemptsList := []UsernamePassword{}

				attemptsList = append(attemptsList, UsernamePassword{
					username: connMeta.User(),
					password: string(password),
				})

				passwordData[connMeta.RemoteAddr()] = PasswordAttemptData{
					usernamePasswords: attemptsList,
					numAttempts:       1,
				}

				if succeed {
					return nil, nil
				}
			} else if passwordData[connMeta.RemoteAddr()].numAttempts == 2 {
				// Get password list and append to it
				pwdList := passwordData[connMeta.RemoteAddr()].usernamePasswords
				pwdList = append(pwdList, UsernamePassword{
					username: connMeta.User(),
					password: string(password),
				})

				// append password attempt
				passwordData[connMeta.RemoteAddr()] = PasswordAttemptData{
					usernamePasswords: pwdList,
					numAttempts:       3,
				}

				// Success
				return nil, nil
			} else {
				// Get password list and append to it
				pwdList := passwordData[connMeta.RemoteAddr()].usernamePasswords
				pwdList = append(pwdList, UsernamePassword{
					username: connMeta.User(),
					password: string(password),
				})

				// append password attempt
				passwordData[connMeta.RemoteAddr()] = PasswordAttemptData{
					usernamePasswords: pwdList,
					numAttempts:       passwordData[connMeta.RemoteAddr()].numAttempts + 1,
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

		go serveSSHConnection(currentConnection, config)
	}
}

func debugPrint(str string) {
	if Dbg {
		fmt.Printf("[DBG] %s\n", str)
	}
}
