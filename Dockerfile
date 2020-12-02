# Pull from latest ubuntu config
FROM ubuntu:20.04

# Install openssh server after updating package config
RUN apt-get update && apt-get install openssh-server -y

# Change root password to "root"
RUN echo 'root:root' | chpasswd

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
