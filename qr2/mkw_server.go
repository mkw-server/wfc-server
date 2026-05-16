package qr2

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"syscall"

	"wwfc/common"
	"wwfc/logging"
)

type MKWServer struct {
	// process of the mkw server. i think this wouldnt work for remove servers
	cmd         *exec.Cmd
	isRemote    bool        // TODO: Unused for now
	udpAddr     net.UDPAddr // the udp address of the mkw server (clients send/receive here)
	conn        net.Conn    // connection to the mkw server's WFC listener
	roomPointer *Room
}

// key is the room address, easy for clients/rooms to lookup
var mkwServers = map[int]*MKWServer{}

func startMKWServer(r *Room) (*MKWServer, error) {
	// for now just use the gamespy address. Only works if wfc-server and mkw-server are on the same machine
	mkwServerIP := net.ParseIP(*common.GetConfig().GameSpyAddress)
	if mkwServerIP == nil {
		return nil, errors.New("failed to parse mkw-server's ip from the config. Please verify it's formatted correctly!")
	}

	// use a random port to be less predictable and also avoid conflicts
	mkwServerPort, err := common.FindOpenUDPPort(26000, 26999)
	if err != nil {
		return nil, fmt.Errorf("failed to find open UDP port: %w", err)
	}

	mkwServerAddr := net.UDPAddr{
		IP:   mkwServerIP,
		Port: mkwServerPort,
	}

	mkwServerPath := common.GetConfig().MkwServerPath

	cmd := exec.Command(
		mkwServerPath,
		"--room-addr", mkwServerAddr.String(),
		"--wfc-addr", mkwServerListener.Addr().String(),
	)

	logging.Info(moduleName, "Running command to start mkw-server process:", cmd)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start mkw-server process: %w", err)
	}

	go func(c *exec.Cmd) {
		err := c.Wait()
		if err != nil {
			logging.Error(moduleName, "mkw-server process exited with error:", err)

		} else {
			logging.Info(moduleName, "mkw-server process exited successfully")
		}
	}(cmd)

	mkwServer := &MKWServer{
		cmd:         cmd,
		isRemote:    false,
		udpAddr:     mkwServerAddr, // set later by ROOM_OPEN message
		conn:        nil,           // set later by ROOM_OPEN message
		roomPointer: r,
	}

	mkwServers[mkwServerPort] = mkwServer

	logging.Info(moduleName, "Created mkw-server for room", r.roomID, "at TCP port", mkwServerPort)

	return mkwServer, nil
}

func (mkwServer *MKWServer) terminateProcess() {
	if mkwServer.cmd == nil && mkwServer.cmd.Process == nil {
		logging.Info(moduleName, "Can't terminate mkw-server process, its already nil (already terminated?)")
		return
	}

	mkwServer.cmd.Process.Signal(syscall.SIGTERM)
	logging.Info(moduleName, "Terminated MKW-Server process!")
}

/*
// sent to clients to let them know the udp address of mkw-server

	type MKWServerAddressPacket struct {
		magic uint32
		ip    int32
		port  uint16
	}
*/
func packMKWServerAddressPacket(addr net.UDPAddr) []byte {
	ip, port := common.IPFormatToInt(addr.String())
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.BigEndian, MKWServerAddressPacketMagic)
	binary.Write(buf, binary.BigEndian, ip)
	binary.Write(buf, binary.BigEndian, port)
	return buf.Bytes()
}
