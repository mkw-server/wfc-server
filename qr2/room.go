package qr2

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/gob"
	"errors"
	"fmt"
	"os"
	"strconv"

	"time"
	"wwfc/common"
	"wwfc/logging"

	"github.com/logrusorgru/aurora/v3"
)

const MaxPlayerCount = 12

type Room struct {
	roomID        uint32
	roomName      string
	CreateTime    time.Time
	Region        common.MKWServerSearchRegion
	gameMode      common.MKWServerGameMode
	LastJoinIndex int
	host          *Player          // only applies for private rooms, authority of room settings
	players       map[*Player]bool // only added for non-guests

	RaceNumber    int
	CourseID      int
	EngineClassID int

	mkwServer       *MKWServer
	aidBitmap       uint32 // available aid bitmap
	numAids         uint32 // num non-guest players
	directAidBitmap uint32 // aid bitmap, including guests.
	suspended       bool   // match making suspension state
	canceled        bool   // whether room is canceled
}

func createRoom(creator *Player, region common.MKWServerSearchRegion, gameMode common.MKWServerGameMode) error {
	if creator == nil {
		return errors.New("Creator (player creating the room) is nil! Can't create room!")
	}

	err := creator.canCreateRoom()
	if err != nil {
		return fmt.Errorf("canCreateRoom() failed with reason: %s", err.Error())
	}

	id := generateRoomID()
	name := strconv.FormatUint(uint64(id), 16)

	room := &Room{
		roomID:          id,
		roomName:        name,
		CreateTime:      time.Now(),
		Region:          region,
		gameMode:        gameMode,
		LastJoinIndex:   0,
		players:         map[*Player]bool{creator: true},
		RaceNumber:      0,
		CourseID:        -1,
		EngineClassID:   -1,
		mkwServer:       nil,
		aidBitmap:       0,
		numAids:         0,
		directAidBitmap: 0,
	}

	logging.Notice(moduleName, "Successfully Created room", room.roomID)

	// only set the host for private rooms
	if region == common.Private {
		room.host = creator
	}

	mkwServer, err := startMKWServer(room)
	if err != nil {
		return fmt.Errorf("mkw-server process failed %d. startMKWServer() %s", room.roomID, err.Error())
	}

	room.mkwServer = mkwServer

	err = room.tryAddPlayer(creator, true)
	if err != nil {
		return err
	}

	rooms[name] = room
	logging.Info(moduleName, "Created room with Region", room.Region)
	return nil
}

// at this point, we've verified that friendsAddedOrOpenHost() returned true, the host is the rooms host according to both the room and player types
func (r *Room) tryAddPlayer(p *Player, isCreator bool) error {
	err := r.cantJoin(p.localPlayerCount)
	if err != nil {
		return fmt.Errorf("Player %d couldn't be added to room %d for reason: %s.", p.PlayerId, r.roomID, err.Error())
	}

	// need to find the next available aid
	aid, err := getAvailableAid(r.aidBitmap)
	if err != nil || aid == NoAid {
		return errors.New("getAvailableAid() errored!")
	}

	r.players[p] = true
	r.aidBitmap = setAid(r.aidBitmap, aid)
	r.directAidBitmap = setAid(r.directAidBitmap, aid)
	r.numAids++

	// if the room private and empty, this player is the host
	var isHost bool = false
	if r.Region == common.Private {
		isHost = r.empty()
	}

	p.setRoomInfo(r, aid, isHost)

	// Send a JoinRoom message to mkw-server to inform them a new player has joined.
	// Only do this here if the player isn't the room's creator. The creator is
	// excluded here since the mkw-server process won't start if the room has just been
	// created --- we have to wait for the process to tell us it started and is ready
	// to add players
	if !isCreator {
		err := r.mkwServer.sendAddPlayerRequest(p)
		if err != nil {
			return fmt.Errorf("mkwServer.sendAddPlayerRequest failed with reason %s", err.Error())
		}
	}

	r.broadcastMatchPackets()
	return nil
}

func (r *Room) removePlayer(p *Player) error {
	if p == nil {
		return errors.New("Can't remove a nil player!")
	}

	if !r.players[p] {
		return fmt.Errorf("Can't remove player %d from room, doesn't exist", p.PlayerId)
	}

	// at this point, a guest is leaving, update the room accordingly
	leaversAid := p.aid

	r.numAids -= 1
	r.aidBitmap = clearAid(r.aidBitmap, leaversAid)
	r.directAidBitmap = clearAid(r.directAidBitmap, leaversAid)
	r.mkwServer.sendRemovePlayerRequest(p)

	if r.shouldCloseRoom(p) {
		r.close()
		// return early since close() resets the player's room fields and broadcasts
		return nil
	}

	p.resetRoomInfo()

	delete(r.players, p)

	r.broadcastMatchPackets()
	return nil
}

func (r *Room) shouldCloseRoom(leavingPlayer *Player) bool {
	if r.host != nil && leavingPlayer == r.host {
		logging.Info(moduleName, "Host left room. Attempting to close it!")
		return true
	}

	if r.numAids == 0 {
		logging.Info(moduleName, "Room has no aids left. Closing!")
		return true
	}

	if r.aidBitmap == 0 {
		logging.Info(moduleName, "Room's aidBitmap is 0. Closing!")
		return true
	}

	if r.directAidBitmap == 0 {
		logging.Info(moduleName, "Room's directAidBitmap is 0. Closing!")
		return true
	}
	// TODO: Check for localPlayerCount after refactor

	return false
}

func (r *Room) cantJoin(localPlayerCount uint32) error {
	if r.full() {
		return errors.New("Room is full")
	}

	if r.suspended {
		return errors.New("Room is suspended")
	}

	if r.canceled {
		return errors.New("Room is canceled")
	}

	if r.numPlayers()+localPlayerCount > MaxPlayerCount {
		return errors.New("Room would be over capacity")
	}

	if r.mkwServer == nil {
		return errors.New("Room.mkwServer is nil")
	}
	return nil
}

// The room's suspension is only changed when all players voted for a suspension thats different than the rooms
func (r *Room) updateSuspension() {
	curRoomSuspension := r.suspended
	for p, exists := range r.players {
		if p == nil || !exists {
			continue
		}

		// player's suspension vote is the same as the room's, return early without changing suspension
		if p.suspendVote == curRoomSuspension {
			return
		}
	}
	// all player's suspension vote differs than the room's, flip the room's suspension

	r.suspended = !r.suspended
	r.broadcastMatchPackets()
	logging.Info(moduleName, "Room", r.roomID, "changed suspension from", !r.suspended, "to", r.suspended)
}

func (r *Room) broadcastMatchPackets() {
	for p, exists := range r.players {
		if p == nil || !exists {
			continue
		}

		if p.roomManagerAddr == "" {
			continue
		}

		// use 0 as the default host aid for public rooms, which the game (probably) needs
		var hostAid uint8 = 0
		if r.host != nil {
			hostAid = r.host.aid
		}
		if r.canceled {
			hostAid = NoAid
		}

		_ = sendToAid(r.aidBitmap, r.numAids, r.directAidBitmap, r.roomID, hostAid, r.suspended, r.canceled, r.localPlayerCounts(), p.aid, p.connIdx)
	}
}

func (r *Room) close() {
	// update the room to an 'empty state', then broadcast it.
	// this must be done before actually deleting it since the players need to be informed
	// after being broadcasted, the players will see the disconnected from room popup
	r.aidBitmap = 0
	r.numAids = 0
	r.directAidBitmap = 0
	r.roomID = 0
	r.suspended = false
	r.canceled = true

	// reset the room related info for the players in the room
	// this is necessary to allow them to join/create rooms again
	for p, exists := range r.players {
		if p == nil || !exists {
			continue
		}

		p.resetRoomInfo()
	}

	if r.mkwServer != nil {
		r.mkwServer.terminateProcess()
	}

	r.broadcastMatchPackets()

	name := r.roomName
	delete(rooms, name)

	logging.Info(moduleName, "Successfully closed room", name)
}

func (r *Room) sendMKWServerJoinRoomForEachPlayer() error {
	mkwServer := r.mkwServer
	if mkwServer == nil {
		return errors.New("Room " + string(r.roomID) + " has a nil mkwServer")
	}
	for p, exists := range r.players {
		if p == nil || !exists {
			continue
		}

		err := mkwServer.sendAddPlayerRequest(p)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *Room) empty() bool {
	return r.numAids == 0
}

func (r *Room) full() bool {
	numPlayers := r.numPlayers()
	if numPlayers > MaxPlayerCount {
		// error case, this is bad
		logging.Error(moduleName, "BAD! Room", r.roomID, "has too many players! Num:", numPlayers)
	}
	return numPlayers == MaxPlayerCount
}

// we can't go by numAids since an aid can have at most 2 players
func (r *Room) numPlayers() uint32 {
	var total uint32 = 0
	for p, exists := range r.players {
		if p != nil && exists {
			total += p.localPlayerCount
		}
	}
	return total
}

func (r *Room) localPlayerCounts() *[MaxPlayerCount]uint32 {
	var ret [MaxPlayerCount]uint32
	for p, exists := range r.players {
		if p == nil || !exists || p.aid > MaxAid {
			continue
		}

		if p.localPlayerCount != 1 && p.localPlayerCount != 2 {
			continue
		}
		ret[p.aid] = uint32(p.localPlayerCount << 24)
	}
	return &ret
}

func ProcessGPStatusUpdate(profileID uint32, senderIP uint64, status string) {
	moduleName := "QR2/GPStatus:" + strconv.FormatUint(uint64(profileID), 10)

	mutex.Lock()
	defer mutex.Unlock()

	login, exists := logins[profileID]
	if !exists || login == nil {
		logging.Info(moduleName, "Received status update for non-existent profile ID", aurora.Cyan(profileID))
		return
	}

	player := login.player
	if player == nil {
		if senderIP == 0 {
			logging.Info(moduleName, "Received status update for profile ID", aurora.Cyan(profileID), "but no player exists")
			return
		}

		// Login with this profile ID
		player, exists = players[senderIP]
		if !exists || player == nil {
			logging.Info(moduleName, "Received status update for profile ID", aurora.Cyan(profileID), "but no player exists")
			return
		}

		if !player.setProfileID(moduleName, strconv.FormatUint(uint64(profileID), 10), "") {
			return
		}
	}

	// Send the client message exploit if not received yet
	if status != "0" && status != "1" && !player.ExploitReceived && player.login != nil && player.login.NeedsExploit {
		playerCopy := *player

		mutex.Unlock()
		logging.Notice(moduleName, "Sending SBCM exploit to DNS patcher client")
		sendClientExploit(moduleName, playerCopy)
		mutex.Lock()
	}
}

func ProcessUSER(senderPid uint32, senderIP uint64, packet []byte) {
	moduleName := "QR2:ProcessUSER/" + strconv.FormatUint(uint64(senderPid), 10)

	mutex.Lock()
	login := logins[senderPid]
	if login == nil {
		mutex.Unlock()
		logging.Warn(moduleName, "Received USER packet from non-existent profile ID", aurora.Cyan(senderPid))
		return
	}

	player := login.player
	if player == nil {
		mutex.Unlock()
		logging.Warn(moduleName, "Received USER packet from profile ID", aurora.Cyan(senderPid), "but no player exists")
		return
	}
	mutex.Unlock()

	miiroomCount := binary.BigEndian.Uint16(packet[0x04:0x06])
	if miiroomCount != 2 {
		logging.Error(moduleName, "Received USER packet with unexpected Mii room count", aurora.Cyan(miiroomCount))
		// Kick the client
		gpErrorCallback(senderPid, "bad_packet")
		return
	}

	miiroomBitflags := binary.BigEndian.Uint32(packet[0x00:0x04])

	var miiData []string
	var miiName []string
	for i := 0; i < int(miiroomCount); i++ {
		if miiroomBitflags&(1<<uint(i)) == 0 {
			continue
		}

		index := 0x08 + i*0x4C
		mii := common.Mii(packet[index : index+0x4C])
		if mii.RFLCalculateCRC() != 0x0000 {
			logging.Error(moduleName, "Received USER packet with invalid Mii data CRC")
			gpErrorCallback(senderPid, "bad_packet")
			return
		}

		createId := binary.BigEndian.Uint64(packet[index+0x18 : index+0x20])
		official, _ := common.RFLSearchOfficialData(createId)
		if official {
			miiName = append(miiName, "Player")
		} else {
			decodedName, err := common.GetWideString(packet[index+0x2:index+0x2+20], binary.BigEndian)
			if err != nil {
				logging.Error(moduleName, "Failed to parse Mii name:", err)
				gpErrorCallback(senderPid, "bad_packet")
				return
			}

			miiName = append(miiName, decodedName)
		}

		miiData = append(miiData, base64.StdEncoding.EncodeToString(packet[index:index+0x4A]))
	}

	mutex.Lock()
	defer mutex.Unlock()

	for i, name := range miiName {
		player.Data["+mii"+strconv.Itoa(i)] = miiData[i]
		player.Data["+mii_name"+strconv.Itoa(i)] = name
	}
}

func ProcessMKWSelectRecord(profileId uint32, key string, value string) {
	moduleName := "QR2:MKWSelectRecord:" + strconv.FormatUint(uint64(profileId), 10)

	mutex.Lock()
	login := logins[profileId]
	if login == nil {
		mutex.Unlock()
		logging.Warn(moduleName, "Received SELECT record from non-existent profile ID", aurora.Cyan(profileId))
		return
	}

	player := login.player
	if player == nil {
		mutex.Unlock()
		logging.Warn(moduleName, "Received SELECT record  from profile ID", aurora.Cyan(profileId), "but no player exists")
		return
	}
	mutex.Unlock()

	room := player.roomPointer
	if room == nil {
		return
	}

	keyColored := aurora.BrightCyan(key).String()

	switch key {
	case "wl:mkw_select_course":
		courseId, err := strconv.ParseUint(value, 10, 32)
		if err != nil {
			logging.Error(moduleName, "Error decoding", keyColored+":", err.Error())
			return
		}

		logging.Info(moduleName, "Selected course", aurora.BrightCyan(strconv.FormatUint(courseId, 10)))

		mutex.Lock()
		defer mutex.Unlock()

		room.RaceNumber++
		room.CourseID = int(courseId)
		room.EngineClassID = -1
		return

	case "wl:mkw_select_cc":
		ccId, err := strconv.ParseUint(value, 10, 32)
		if err != nil {
			logging.Error(moduleName, "Error decoding", keyColored+":", err.Error())
			return
		}

		logging.Info(moduleName, "Selected CC", aurora.BrightCyan(strconv.FormatUint(ccId, 10)))

		mutex.Lock()
		defer mutex.Unlock()

		room.EngineClassID = int(ccId)
		return
	}

}

// saveRooms saves the current rooms state to disk.
// Expects the mutex to be locked.
func saveRooms() error {
	file, err := os.OpenFile("state/qr2_rooms.gob", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}

	encoder := gob.NewEncoder(file)
	err = encoder.Encode(rooms)
	file.Close()
	return err
}

// loadRooms loads the rooms state from disk.
// Expects the mutex to be locked, and the players to already be loaded.
func loadRooms() error {
	file, err := os.Open("state/qr2_rooms.gob")
	if err != nil {
		return err
	}

	decoder := gob.NewDecoder(file)
	err = decoder.Decode(&rooms)
	file.Close()
	if err != nil {
		return err
	}

	for _, player := range players {
		if player.roomPointer != nil || player.RoomName == "" {
			continue
		}

		room := rooms[player.RoomName]
		if room == nil {
			logging.Warn("QR2", "player", aurora.BrightCyan(player.Addr.String()), "has a room name but the room does not exist")
			continue
		}

		if room.players == nil {
			room.players = map[*Player]bool{}
		}

		room.players[player] = true
		player.roomPointer = room
	}

	return nil
}

func shutdownMKWServerServers() {
	for _, r := range rooms {
		mkwServer := r.mkwServer
		if mkwServer != nil {
			mkwServer.terminateProcess()
		}
	}
}
