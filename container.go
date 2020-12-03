package main

import (
	"context"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
)

func IsSSHRunning(ctx context.Context, cli *client.Client, container string) (bool, error) {
	res, err := cli.ContainerInspect(ctx, container)
	if err != nil {
		return false, err
	}

	return res.State.Health.Status == "healthy", nil
}

func CreateAndStartNewContainer(cli *client.Client) (string, error) {
	ctx := context.Background()

	resp, err := cli.ContainerCreate(ctx, &container.Config{
		Image:    "sshh",
		Hostname: "ecorp-finances",
		ExposedPorts: nat.PortSet{
			"22/tcp": struct{}{},
		},
	}, &container.HostConfig{
		PortBindings: nat.PortMap{
			"22/tcp": []nat.PortBinding{
				{
					HostIP:   "127.0.0.1",
					HostPort: "1337",
				},
			},
		},
	}, nil, "")

	// Return an error, if any
	if err != nil {
		return "", err
	}

	// Start the container with the specific ID
	err = cli.ContainerStart(ctx, resp.ID, types.ContainerStartOptions{})

	// Check for errors
	if err != nil {
		return resp.ID, err
	}

	// Unfortunately, we're in a bit of a tizzy here. We currently have no
	// reliable way to detect when the container is open. On Linux, we can
	// get around this by simply connecting to port 22 and waiting, but on
	// macOS, we probably have to rely on HEALTHCHECK and busy-waiting

	// Maybe get around this by making a pool of docker containers, and just
	// pick one per connection? Then, every new connection will always have
	// a container to connect to. The issue then becomes for known connections,
	// but I suppose we can simply use busy waiting since it's a less common
	// case
	for {
		isRunning, err := IsSSHRunning(ctx, cli, resp.ID)

		// Check for errors
		if err != nil {
			return resp.ID, err
		}

		// Break if running
		if isRunning {
			break
		}
	}

	if err != nil {
		return resp.ID, err
	}

	return resp.ID, nil
}

func CreateConnection() *client.Client {
	// A good place to get up and running: https://docs.docker.com/engine/api/sdk/
	// Create new docker client
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())

	// If error, we do nothing
	if err != nil {
		debugPrint("Unable to create the docker connection")
		return nil
	}

	return cli
}
