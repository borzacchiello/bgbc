package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"time"
)

type room struct {
	name  string
	peers []net.Conn
}

type serverContext struct {
	rooms map[string]*room
}

func (r *room) isFull() bool {
	return len(r.peers) == 2
}

func (r *room) handlePeer(ctx *serverContext, peerNum int) {
	var elapsed time.Duration
	waitByte := byte(42)
	bothConnectedByte := byte(43)

	// Notify the peer when both are connected
	for len(r.peers) < 2 {
		time.Sleep(100 * time.Millisecond)
		elapsed += 100 * time.Millisecond
		if elapsed%(1*time.Second) == 0 {
			_, err := r.peers[peerNum-1].Write([]byte{waitByte})
			if err != nil {
				log.Printf("room %s: peer%d disconnected while waiting for peer2: %s\n", r.name, peerNum, err)
				delete(ctx.rooms, r.name)
				return
			}
		}
	}
	// Send both connected byte and peer number
	_, err := r.peers[peerNum-1].Write([]byte{bothConnectedByte, byte(peerNum)})
	if err != nil {
		log.Printf("room %s: peer%d disconnected while notifying both connected: %s\n", r.name, peerNum, err)
		delete(ctx.rooms, r.name)
		return
	}

	fmt.Printf("room %s: both peers connected, starting relay (peer%d)\n", r.name, peerNum)

	src := r.peers[peerNum-1]
	dst := r.peers[1-(peerNum-1)]
	defer src.Close()
	defer dst.Close()

	recvBuf := make([]byte, 1024)
	for {
		n, err := src.Read(recvBuf)
		if err != nil {
			log.Printf("room %s: peer%d read error: %s", r.name, peerNum, err)
			break
		}
		_, err = dst.Write(recvBuf[:n])
		if err != nil {
			log.Printf("room %s: peer%d write error: %s", r.name, peerNum, err)
			break
		}
	}

	if peerNum == 1 {
		log.Printf("room %s: deleting the room", r.name)
		delete(ctx.rooms, r.name)
	}
}

func main() {
	argsWithoutProg := os.Args[1:]
	if len(argsWithoutProg) != 1 {
		log.Fatalf("usage: serialServer <ip:port>")
		os.Exit(1)
	}

	ctx := &serverContext{
		rooms: make(map[string]*room),
	}

	listen, err := net.Listen("tcp", argsWithoutProg[0])
	if err != nil {
		log.Fatal(err)
		os.Exit(1)
	}
	defer listen.Close()
	log.Printf("listening on %s", argsWithoutProg[0])

	roomNameBuf := make([]byte, 40)
	for {
		conn, err := listen.Accept()
		if err != nil {
			log.Printf("!Err: failed to accept: %s", err)
			continue
		}
		_, err = io.ReadFull(conn, roomNameBuf)
		if err != nil {
			log.Printf("!Err: failed to read room name: %s", err)
			conn.Close()
			continue
		}

		roomName := string(bytes.TrimRight(roomNameBuf, "\x00"))
		r, ok := ctx.rooms[roomName]
		if !ok {
			r = &room{name: roomName}
			ctx.rooms[roomName] = r
			log.Printf("created room %s", roomName)
		}

		if r.isFull() {
			log.Printf("room %s is full, rejecting connection from %s", roomName, conn.RemoteAddr().String())
			conn.Close()
			continue
		}

		log.Printf("room %s: accepted connection from %s", roomName, conn.RemoteAddr().String())
		r.peers = append(r.peers, conn)
		go r.handlePeer(ctx, len(r.peers))
	}
}
