package main

import (
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	bencode "github.com/jackpal/bencode-go"
)

func cmdDecode() {
	bencodedValue := os.Args[2]

	decoded, err := bencode.Decode(strings.NewReader(bencodedValue))
	if err != nil {
		fmt.Println(err)
		return
	}

	jsonOutput, _ := json.Marshal(decoded)
	fmt.Println(string(jsonOutput))
}

func cmdInfo() {
	torrentFile, err := os.Open(os.Args[2])
	if err != nil {
		fmt.Println(err)
		return
	}

	defer torrentFile.Close()

	type TorrentInfo struct {
		Length      int    `bencode:"length"`
		Name        string `bencode:"name"`
		PieceLength int    `bencode:"piece length"`
		Pieces      string `bencode:"pieces"`
	}

	type Torrent struct {
		Announce string      `bencode:"announce"`
		Info     TorrentInfo `bencode:"info"`
	}

	var torrent Torrent
	err = bencode.Unmarshal(torrentFile, &torrent)
	if err != nil {
		fmt.Println(err)
		return
	}

	fmt.Printf("Tracker URL: %s\n", torrent.Announce)
	fmt.Printf("Length: %d\n", torrent.Info.Length)

	hash := sha1.New()
	err = bencode.Marshal(hash, torrent.Info)
	if err != nil {
		fmt.Println(err)
		return
	}

	fmt.Printf("Info Hash: %x\n", hash.Sum(nil))
	fmt.Printf("Piece Length: %d\n", torrent.Info.PieceLength)
	fmt.Println("Pieces Hashes:")
	for i := 0; i < len(torrent.Info.Pieces); i += 20 {
		fmt.Printf("%x\n", torrent.Info.Pieces[i:i+20])
	}
}

func main() {
	command := os.Args[1]

	if command == "decode" {
		cmdDecode()
	} else if command == "info" {
		cmdInfo()
	} else {
		fmt.Println("Unknown command: " + command)
		os.Exit(1)
	}
}
