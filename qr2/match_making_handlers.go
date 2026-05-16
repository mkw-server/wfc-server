package qr2

import (
	"errors"
	"fmt"

	"wwfc/common"
	"wwfc/logging"

	"github.com/logrusorgru/aurora/v3"
)

// request to open a private room
func handleOpenRoomRequest(host *Player) error {
	err := createRoom(host, common.Private, common.Undecided)
	if err != nil {
		return fmt.Errorf("createRoom() failed for player %d with message %s", host.PlayerId, err.Error())
	}
	return nil
}

func handleJoinFriendRequest(joiner *Player, request *JoinFriendRequest) error {
	logging.Info(moduleName, "Player wants to join a friend", aurora.Cyan(request.friendProfileId), "and searchRegion", request.searchRegion)

	l := logins[request.friendProfileId]
	if l == nil {
		return errors.New("Login with friendProfileId doesn't exist")
	}

	err := joiner.canJoinFriend(l)
	if err != nil {
		return err
	}

	friend := l.player
	r := friend.roomPointer
	if r == nil {
		return errors.New("Friend isn't in a room")
	}

	if r.Region != request.searchRegion {
		return fmt.Errorf("Region mismatch. Requested is %d while friend's is %d", request.searchRegion, r.Region)
	}

	if r.Region == common.Private && r.host != friend {
		return errors.New("Rooms host isn't the expected host!")
	}

	return r.tryAddPlayer(joiner, false)
}

func handleLeaveRoomRequest(p *Player) error {
	r := p.roomPointer
	if r == nil {
		return fmt.Errorf("Player %d sent a LeaveRoom request when they're roomless!")
	}

	return r.removePlayer(p)
}

func handleSuspendRequest(p *Player, requestSuspend bool) error {
	if p.suspendVote == requestSuspend {
		return nil
	}

	r := p.roomPointer
	if r == nil {
		return fmt.Errorf("Player %d requested to suspend (value: %d ) but doesn't belong to a room", p.PlayerId, requestSuspend)
	}

	p.suspendVote = requestSuspend

	// Try to update room suspension
	r.updateSuspension()
	return nil
}

func handleSearchPublicRoomRequest(p *Player, searchReq *SearchPublicRoomRequest) error {
	if p.roomPointer != nil {
		return fmt.Errorf("Player %d requested to search for a room, but their roomPointer isn't nil (room %d)", p.PlayerId, p.roomPointer.roomID)
	}

	return findPublicRoom(p, searchReq.region, searchReq.mode)
}

func handleSetLocalPlayerCount(p *Player, lpc uint8) error {
	err := p.setLocalPlayers(lpc)

	return err
}
