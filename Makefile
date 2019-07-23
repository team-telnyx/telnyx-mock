.PHONY: all clean generate test vet lint check-gofmt

all: clean generate test vet lint check-gofmt

clean:
	rm -rf bindata.go dist/ telnyx-mock
	go clean -i -testcache -cache

generate:
	go generate

test:
	go test ./...

vet:
	go vet ./...

# This command is overcomplicated because Golint's `./...` doesn't filter
# `vendor/` (unlike every other Go command).
lint:
	go list ./... | xargs -I{} -n1 sh -c 'golint -set_exit_status {} || exit 255'

check-gofmt:
	scripts/check_gofmt.sh

####################
container:
	docker build . -t telnyx-mock
	docker run -p 12111-12112:12111-12112 telnyx-mock
