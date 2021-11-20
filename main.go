package main

// create discordgo session and connect to discord and import discordgo package
import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/bwmarrin/discordgo"
	// import godotenv package to read .env file
	"github.com/joho/godotenv"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v3/pkg/media"
	"github.com/pion/webrtc/v3/pkg/media/oggwriter"
)

// set prefix variable with value "!c"
var prefix string = "!c"

// set map for store command and function
var commandMap = map[string]func(*discordgo.Session, *discordgo.MessageCreate){
	"ping":       ping,
	"setPrefix":  setPrefix,
	"join":       joinVoice,
	"disconnect": disconnect,
	"avatar":     displayAvatar,
}

// create discordgo session and connect to discord
func main() {
	// godotenv load environment variables by path .env
	err := godotenv.Load(".env")
	if err != nil {
		fmt.Println("Error loading .env file")
	}

	// create discordgo session
	dg, err := discordgo.New("Bot " + os.Getenv("TOKEN"))
	if err != nil {
		fmt.Println("error creating discord session,", err)
		return
	}

	// connect to discord
	err = dg.Open()
	if err != nil {
		fmt.Println("error opening connection,", err)
		return
	}

	// call multiCommand function
	dg.AddHandler(multiCommand)

	// wait for a signal to quit
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-sc

	// close discord connection
	dg.Close()
}

func createPionRTPPacket(p *discordgo.Packet) *rtp.Packet {
	return &rtp.Packet{
		Header: rtp.Header{
			Version: 2,
			// Taken from Discord voice docs
			PayloadType:    0x78,
			SequenceNumber: p.Sequence,
			Timestamp:      p.Timestamp,
			SSRC:           p.SSRC,
		},
		Payload: p.Opus,
	}
}

// create multiCommand function with parameter *discordgo.Session and *discordgo.MessageCreate
// multiCommand function will check if message start with prefix and call function in commandMap
func multiCommand(s *discordgo.Session, m *discordgo.MessageCreate) {
	// check if message start with prefix
	if strings.HasPrefix(m.Content, prefix) {
		// split message into array
		message := strings.Split(m.Content, " ")
		// get command from array
		command := message[1]
		// get command function from commandMap
		f, ok := commandMap[command]
		if !ok {
			unknownMessage(s, m)
			return
		}
		// call command function
		f(s, m)
	}
}

// create function handleVoice with parameter chan *discordgo.Packet
func handleVoice(packet chan *discordgo.Packet) {
	files := make(map[uint32]media.Writer)
	for p := range packet {
		file, ok := files[p.SSRC]
		if !ok {
			var err error
			file, err = oggwriter.New(fmt.Sprintf("%d.ogg", p.SSRC), 48000, 2)
			if err != nil {
				fmt.Printf("failed to create file %d.ogg, giving up on recording: %v\n", p.SSRC, err)
				return
			}
			files[p.SSRC] = file
		}
		// Construct pion RTP packet from DiscordGo's type.
		rtp := createPionRTPPacket(p)
		err := file.WriteRTP(rtp)
		if err != nil {
			fmt.Printf("failed to write to file %d.ogg, giving up on recording: %v\n", p.SSRC, err)
		}
	}

	// Once we made it here, we're done listening for packets. Close all files
	for _, f := range files {
		f.Close()
	}
}

// create unknown message function
func unknownMessage(s *discordgo.Session, m *discordgo.MessageCreate) {
	// send message to channel
	s.ChannelMessageSend(m.ChannelID, "unknown command")
}

// create ping command
func ping(s *discordgo.Session, m *discordgo.MessageCreate) {
	s.ChannelMessageSend(m.ChannelID, "pong")
}

// create setPrefix command
func setPrefix(s *discordgo.Session, m *discordgo.MessageCreate) {
	// split message into array
	message := strings.Split(m.Content, " ")
	// set prefix
	prefix = message[1]
	// reply with prefix
	s.ChannelMessageSend(m.ChannelID, "prefix is now "+prefix)
}

// create joinVoice command
func joinVoice(s *discordgo.Session, m *discordgo.MessageCreate) {
	// get guild id
	guildID := s.State.Guilds[0].ID

	// get current voice channel
	channelID := s.State.Guilds[0].VoiceStates[0].ChannelID

	// set bot to join voice channel and reply with channel id
	// handle voice and error
	vc, err := s.ChannelVoiceJoin(guildID, channelID, false, false)
	if err != nil {
		fmt.Println(err)
	}

	// go func() {
	// 	time.Sleep(10 * time.Second)
	// 	close(vc.OpusRecv)
	// 	vc.Close()
	// }()

	handleVoice(vc.OpusRecv)
}

// create function to disconnect from current voice channel
func disconnect(s *discordgo.Session, m *discordgo.MessageCreate) {
	// get guild id
	guildID := s.State.Guilds[0].ID

	// join bot to voice channel
	s.ChannelVoiceJoin(guildID, "", false, false)
}

// create function to display user avatar
func displayAvatar(s *discordgo.Session, m *discordgo.MessageCreate) {
	// create variable for user
	var user *discordgo.User

	// get user mention length
	mentionLength := len(m.Mentions)
	// check if mention length is greater than 0
	if mentionLength > 0 {
		// get user id
		userID := m.Mentions[0].ID
		// get user
		user, _ = s.User(userID)
	} else {
		// get user id
		userID := m.Author.ID
		// get user
		user, _ = s.User(userID)
	}

	// send message to channel
	s.ChannelMessageSend(m.ChannelID, user.AvatarURL(""))
}
