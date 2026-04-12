go version
go mod tidy
#go build -ldflags="-s -w" -o ZeroBot-Plugin
go generate main.go
go run  -ldflags "-s -w" main.go
