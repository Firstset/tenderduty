TEST_CASE=.

build:
	CGO_CFLAGS="-Wno-deprecated-declarations" go build -ldflags '-s -w' -trimpath -o ./tenderduty main.go

test:
	CGO_CFLAGS="-Wno-deprecated-declarations" go test -v -run "$(TEST_CASE)" ./...
