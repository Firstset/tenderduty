build:
	CGO_CFLAGS="-Wno-deprecated-declarations" go build -ldflags '-s -w' -trimpath -o ./tenderduty main.go
