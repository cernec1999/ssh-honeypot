package main

import (
	"context"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
)

// IsSSHRunning returns true if the container is running.
func IsSSHRunning(container string) (bool, error) {
	cli, err := CreateConnection()

	// Error handling
	if err != nil {
		return false, err
	}

	defer cli.Close()

	res, err := cli.ContainerInspect(context.Background(), container)
	if err != nil {
		return false, err
	}

	return res.State.Health.Status == "healthy", nil
}

// GetContainerIP returns the port of a container.
func GetContainerIP(container string) (string, error) {
	cli, err := CreateConnection()

	// Error handling
	if err != nil {
		return "", err
	}

	defer cli.Close()

	res, err := cli.ContainerInspect(context.Background(), container)
	if err != nil {
		return "", err
	}

	return res.NetworkSettings.Networks["no-internet"].IPAddress, nil
}

// StopContainer stops the container
func StopContainer(containerName string) error {
	cli, err := CreateConnection()

	// Error handling
	if err != nil {
		return err
	}

	defer cli.Close()

	timeDuration, err := time.ParseDuration("5s")

	if err != nil {
		return err
	}

	cli.ContainerStop(context.Background(), containerName, &timeDuration)

	return nil
}

// StartExistingContainer starts the container
func StartExistingContainer(containerName string) error {
	cli, err := CreateConnection()

	// Error handling
	if err != nil {
		return err
	}

	defer cli.Close()

	// Inspect container attributes
	res, err := cli.ContainerInspect(context.Background(), containerName)

	if err != nil {
		return err
	}

	// If the container is running, do nothing
	if res.State.Running {
		return nil
	}

	// Start the container with a specific ID
	err = cli.ContainerStart(context.Background(), containerName, types.ContainerStartOptions{})

	// Check for errors
	if err != nil {
		return err
	}

	cli.Close()

	return nil
}

// CreateAndStartNewContainer creates a new container
func CreateAndStartNewContainer() (string, error) {
	cli, err := CreateConnection()

	// Error handling
	if err != nil {
		return "", err
	}

	defer cli.Close()

	resp, err := cli.ContainerCreate(context.Background(), &container.Config{
		Image:    "sshh",
		Hostname: "ecorp-finances",
		ExposedPorts: nat.PortSet{
			"22/tcp": struct{}{},
		},
	}, &container.HostConfig{
		// Should we make the user create a docker network?
		NetworkMode: "no-internet",
	}, nil, "")

	// Return an error, if any
	if err != nil {
		return resp.ID, err
	}

	// Start the container with the specific ID
	err = cli.ContainerStart(context.Background(), resp.ID, types.ContainerStartOptions{})

	// Check for errors
	if err != nil {
		return resp.ID, err
	}

	return resp.ID, nil
}

// CreateConnection creates a new docker connection
func CreateConnection() (*client.Client, error) {
	// A good place to get up and running: https://docs.docker.com/engine/api/sdk/
	// Create new docker client
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())

	// If error, we do nothing
	if err != nil {
		debugPrint("Unable to create the docker connection")
		return nil, err
	}

	return cli, nil
}
