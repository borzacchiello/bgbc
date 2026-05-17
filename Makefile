all: bgbc serialServer

serialServer: cmd/serial/main.go
	go build -o serialServer cmd/serial/main.go

bgbc: cmd/sdl/main.go
	go build -o bgbc cmd/sdl/main.go

wasm: cmd/wasm/main.go
	GOOS=js GOARCH=wasm go build -o web/assets/bgbc.wasm cmd/wasm/main.go

clean:
	rm -f bgbc serialServer web/assets/bgbc.wasm *.exe
