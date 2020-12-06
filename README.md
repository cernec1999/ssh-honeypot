# SSH Honeypot
SSH Honeypot is a Golang project that intercepts SSH requests and proxies them to specialized docker containers. The proxy is able to log data being sent to and from the connection in real-time, and attributes regarding the connection, including IP address, country of origin, and usernames and passwords specified.

Unlike other honeypots, this project is not stateless, meaning the subsequent SSH connections from the same IP address will run in the same docker container.

The point of this project is to detect bots attempting to brute force username and password information, and let them in to see what commands they run and how they interface with a machine.

# Building
```$ make build```

# Running
```$ ./sshh```

# MacOS
Please note, that at its current revision, this ssh-honeypot is not supported on macOS. This may be solved in a future release. The reason for this is that there is no docker0 bridge on macOS, which means that we cannot connect to our containers using their internal IP addresses.

One possible solution to this is to have our containers bridge their network with the host. I see this as a security issue, as they will be exposed to the WAN, including devices on the network.

Another possible solution is to Dockerize the Golang script, and expose the Docker socket from the host to the container. I don't like this solution.

The last solution is to implement IP table rules on the host. Do not allow traffic except for SSH incoming. Do not implement these rules on the container itself, as the attacker can change these settings.

# Inspiration / Credits
* [Golang](https://golang.org/)
* [sshproxy](https://github.com/dutchcoders/sshproxy/)
* [sqlite3](https://github.com/mattn/go-sqlite3)
