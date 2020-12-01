PROGRAM=sshh
SOURCE=*.go

build:
	go build -o $(PROGRAM) $(SOURCE)

clean:
	rm -f $(PROGRAM)

fmt:
	gofmt -w $(SOURCE)

vet:
	go vet $(SOURCE)

# TODO: Add install
