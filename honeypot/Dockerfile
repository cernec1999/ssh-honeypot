# Pull from latest ubuntu config
FROM ubuntu:20.04

# Install openssh server after updating package config
RUN apt-get update && apt-get install openssh-server net-tools -y

# Change root password to "root"
RUN echo 'root:root' | chpasswd

# Create healthcheck to ensure openssh is running
# https://stackoverflow.com/questions/46362935/how-to-add-a-docker-health-check-to-test-a-tcp-port-is-open
HEALTHCHECK --interval=100ms  CMD netstat -an | grep 22 > /dev/null; if [ 0 != $? ]; then exit 1; fi;

# Configure SSH to allow root login and disable PAM
RUN sed -i 's|\s*[#]\s*PermitRootLogin .*|PermitRootLogin yes|g' /etc/ssh/sshd_config
RUN sed -i 's|\s*[#]\s*UsePAM .*|UsePAM yes|g' /etc/ssh/sshd_config

# Start SSH and expose ports
RUN service ssh start
EXPOSE 22
CMD ["/usr/sbin/sshd","-D"]

# To run this now, we can use:
# docker build . --tag sshh
# docker run -d -p 1337:22 sshh
