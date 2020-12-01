# SSH Honeypot
SSH Honeypot is a Golang project that intercepts SSH requests and proxies them to specialized docker containers. The proxy is able to log data being sent to and from the connection in real-time, and attributes regarding the connection, including IP address, country of origin, and usernames and passwords specified.

Unlike other honeypots, this project is not stateless, meaning the subsequent SSH connections will run in the same docker container.

The point of this project is to detect bots attempting to brute force username and password information, and let them in to see what commands they run and how they interface with a machine.

# Installing dependencies
To install the Golang dependencies, you can simply run the following command.
```$ go get -d .```

# Building
```$ go build .```

# Running
```$ go run .```