package main

// create discordgo session and connect to discord and import discordgo package
import (
	"context"
	"fmt"
	"log"
	"net/url"
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

	// import waterlink package
	"github.com/lukasl-dev/waterlink"

	"github.com/lukasl-dev/waterlink/entity/event"
	"github.com/lukasl-dev/waterlink/entity/player"
	"github.com/lukasl-dev/waterlink/entity/server"
	"github.com/lukasl-dev/waterlink/usecase/play"
)

// set prefix variable with value "!c"
var prefix string = "!c"

// create variable waterlinkConn
var waterlinkConn waterlink.Connection

// create water link requester variable
var waterlinkRequester waterlink.Requester

// create sessionID variable
var sessionID string

// create discordgo session variable
var dg *discordgo.Session

// set map for store command and function
var commandMap = map[string]func(*discordgo.Session, *discordgo.MessageCreate){
	"ping":       ping,
	"prefix":     setPrefix,
	"join":       joinVoice,
	"disconnect": disconnect,
	"avatar":     displayAvatar,
	"help":       help,
	"music":      music,
}

// set map for store music command and function
var musicCommandMap = map[string]func(*discordgo.Session, *discordgo.MessageCreate){
	"play":   playMusic,
	"pause":  pauseMusic,
	"stop":   stopMusic,
	"resume": resumeMusic,
}

var rootHelp = []*discordgo.MessageEmbedField{
	// create utility command help
	{
		Name:   "Utility Commands",
		Value:  "`ping`, `prefix`, `avatar`",
		Inline: false,
	},
	// create voice command help
	{
		Name:   "Voice Commands",
		Value:  "`join`, `disconnect`",
		Inline: false,
	},
}

// create discordgo session and connect to discord
func main() {
	// godotenv load environment variables by path .env
	err := godotenv.Load(".env")
	if err != nil {
		fmt.Println("Error loading .env file")
	}

	// create discordgo session
	dg, err = discordgo.New("Bot " + os.Getenv("TOKEN"))
	if err != nil {
		fmt.Println("error creating discord session,", err)
		return
	}

	// call handleReady function
	dg.AddHandler(handleReady)
	// call handleVoiceUpdate function
	dg.AddHandler(handleVoiceUpdate)
	// call multiCommand function
	dg.AddHandler(multiCommand)

	// connect to discord
	err = dg.Open()
	if err != nil {
		// panic if error
		panic(err)
	}

	connHost := url.URL{
		Scheme: os.Getenv("WATERLINK_SCHEME"),
		Host:   os.Getenv("WATERLINK_HOST"),
	}

	reqHost := url.URL{
		Scheme: os.Getenv("WATERLINK_REQ_SCHEME"),
		Host:   os.Getenv("WATERLINK_REQ_HOST"),
	}

	opts := waterlink.NewConnectOptions().WithPassphrase(os.Getenv("WATERLINK_PASSPHRASE"))
	waterlinkConn, err = waterlink.Connect(context.TODO(), connHost, opts)
	if err != nil {
		panic(err)
	}

	optsReq := waterlink.NewRequesterOptions().WithPassphrase(os.Getenv("WATERLINK_REQ_PASSPHRASE"))
	waterlinkRequester = waterlink.NewRequester(reqHost, optsReq)

	fmt.Println("Connected to application server")

	go func(conn waterlink.Connection) {
		for evt := range conn.Events() {
			switch evt.Type() {
			case event.Stats:
				evt := evt.(server.Stats)
				println("Server uses", evt.Memory.Used, "memory")
				println("Server uses", evt.CPU.Cores, "cpu")
				println("Server uses", evt.PlayingPlayers, "playing players")
			case event.TrackStart:
				evt := evt.(player.TrackStart)
				println("Track", evt.TrackID, "started on guild", evt.GuildID)
			case event.TrackStuck:
				evt := evt.(player.TrackStuck)
				println("Track", evt.TrackID, "stuck on guild", evt.GuildID)
			case event.TrackEnd:
				evt := evt.(player.TrackEnd)
				println("Track", evt.TrackID, "end on guild", evt.GuildID)
			case event.TrackException:
				evt := evt.(player.TrackException)
				println("Track", evt.TrackID, "exception error", evt.GuildID, evt.Error)
			}
		}
	}(waterlinkConn)

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
func multiCommand(_ *discordgo.Session, m *discordgo.MessageCreate) {
	// check if message start with prefix
	if strings.HasPrefix(m.Content, prefix) {
		// split message into array
		message := strings.Split(m.Content, " ")
		// get command from array
		command := message[1]
		// get command function from commandMap
		f, ok := commandMap[command]
		if !ok {
			unknownMessage(dg, m)
			return
		}
		// call command function
		f(dg, m)
	}
}

// handleReady handles the ready event.
func handleReady(_ *discordgo.Session, ready *discordgo.Ready) {
	sessionID = ready.SessionID
}

// handleVoiceUpdate handles the voice server update event.
func handleVoiceUpdate(_ *discordgo.Session, update *discordgo.VoiceServerUpdate) {
	err := waterlinkConn.UpdateVoice(update.GuildID, sessionID, update.Token, update.Endpoint)
	if err != nil {
		log.Printf("Updating voice server failed on guild %s: %s\n", update.GuildID, err)
	} else {
		log.Printf("Updated voice server of guild %s.\n", update.GuildID)
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
func unknownMessage(_ *discordgo.Session, m *discordgo.MessageCreate) {
	// send message to channel
	dg.ChannelMessageSend(m.ChannelID, "unknown command")
}

// create ping command
func ping(_ *discordgo.Session, m *discordgo.MessageCreate) {
	dg.ChannelMessageSend(m.ChannelID, "pong")
}

// create setPrefix command
func setPrefix(_ *discordgo.Session, m *discordgo.MessageCreate) {
	// split message into array
	message := strings.Split(m.Content, " ")
	// check if message length is 2
	if len(message) > 2 {
		// set prefix variable with value from array
		prefix = message[2]

		// reply with prefix
		dg.ChannelMessageSend(m.ChannelID, fmt.Sprintf("prefix set to **%s**", prefix))
	} else {
		// reply with current prefix
		dg.ChannelMessageSend(m.ChannelID, fmt.Sprintf("prefix is **%s**", prefix))
	}
}

// create joinVoice command
func joinVoice(_ *discordgo.Session, m *discordgo.MessageCreate) {
	// get guild id
	guildID := m.GuildID

	vcID := findMembersChannel(guildID, m.Author.ID)

	// set bot to join voice channel and reply with channel id
	// handle voice and error
	// create voice connection
	err := dg.ChannelVoiceJoinManual(guildID, vcID, false, false)
	if err != nil {
		fmt.Println(err)
	}

	// go func() {
	// 	time.Sleep(10 * time.Second)
	// 	close(vc.OpusRecv)
	// 	vc.Close()
	// }()

	// vc.OpusSend <- []byte{0xF8, 0xFF, 0xFE, 0xF8, 0xFF, 0xFE, 0xF8, 0xFF, 0xFE, 0xF8, 0xFF, 0xFE}

	// handleVoice(vc.OpusRecv)
}

// create function to disconnect from current voice channel
func disconnect(_ *discordgo.Session, m *discordgo.MessageCreate) {
	// get guild id
	guildID := m.GuildID

	// join bot to voice channel
	dg.ChannelVoiceJoinManual(guildID, "", false, false)
}

// create function to display user avatar
func displayAvatar(_ *discordgo.Session, m *discordgo.MessageCreate) {
	// create variable for user
	var user *discordgo.User

	// get user mention length
	mentionLength := len(m.Mentions)
	// check if mention length is greater than 0
	if mentionLength > 0 {
		// get user id
		userID := m.Mentions[0].ID
		// get user
		user, _ = dg.User(userID)
	} else {
		// get user id
		userID := m.Author.ID
		// get user
		user, _ = dg.User(userID)
	}

	// send message to channel
	dg.ChannelMessageSend(m.ChannelID, user.AvatarURL(""))
}

// create help function to display help message as embed
func help(_ *discordgo.Session, m *discordgo.MessageCreate) {
	// create embed
	embed := &discordgo.MessageEmbed{
		Title:  "Copilot Help",
		Color:  0x00ff00,
		Fields: rootHelp,
	}

	// send embed to channel
	dg.ChannelMessageSendEmbed(m.ChannelID, embed)
}

// create music function to handle music commands
func music(_ *discordgo.Session, m *discordgo.MessageCreate) {
	// split message into array
	message := strings.Split(m.Content, " ")
	// check if message length less than 2
	if len(message) < 2 {
		// reply with error message
		unknownMessage(dg, m)
		return
	}

	// get command from array
	command := message[2]
	// get command function from commandMap
	f, ok := musicCommandMap[command]
	if !ok {
		unknownMessage(dg, m)
		return
	}
	// call command function
	f(dg, m)
}

// create playMusic function to handle play command
func playMusic(_ *discordgo.Session, m *discordgo.MessageCreate) {
	// split message into array
	message := strings.SplitN(m.Content, " ", 4)
	// check if message length is 3
	if len(message) < 3 {
		// reply with error message
		unknownMessage(dg, m)
		return
	}

	// get guild id
	guildID := m.GuildID

	// get channel id
	channelID := m.ChannelID

	// get url from array
	title := message[3]

	// load track from waterlinkrequester
	tracks, err := waterlinkRequester.LoadTracks(title)
	if err != nil {
		fmt.Println(err)
		return
	}

	vcID := findMembersChannel(guildID, m.Author.ID)

	// create voice connection
	err = dg.ChannelVoiceJoinManual(guildID, vcID, false, false)
	if err != nil {
		fmt.Println(err)
	}

	// create waterlink opts
	opts := play.NewOptions().WithVolume(100)

	// waterlinkconn play music by title
	// and handle error
	if err := waterlinkConn.Play(guildID, tracks.Tracks[0].ID, opts); err != nil {
		fmt.Println(err)
	}

	dg.ChannelMessageSend(channelID, fmt.Sprintf("Now playing %s.", tracks.Tracks[0].Info.Title))

}

// create pauseMusic function to handle pause command
func pauseMusic(_ *discordgo.Session, m *discordgo.MessageCreate) {
	// get guild id
	guildID := m.GuildID

	// waterlinkconn pause music
	// and handle error
	if err := waterlinkConn.SetPaused(guildID, true); err != nil {
		fmt.Println(err)
	}

	// send message to channel
	dg.ChannelMessageSend(m.ChannelID, "music paused")
}

// create resumeMusic function to handle resume command
func resumeMusic(_ *discordgo.Session, m *discordgo.MessageCreate) {
	// get guild id
	guildID := m.GuildID

	// waterlinkconn resume music
	// and handle error
	if err := waterlinkConn.SetPaused(guildID, false); err != nil {
		fmt.Println(err)
	}

	// send message to channel
	dg.ChannelMessageSend(m.ChannelID, "music resumed")
}

// create stopMusic function to handle stop command
func stopMusic(_ *discordgo.Session, m *discordgo.MessageCreate) {
	// get guild id
	guildID := m.GuildID

	// waterlinkconn stop music
	// and handle error
	if err := waterlinkConn.Stop(guildID); err != nil {
		fmt.Println(err)
	}

	// send message to channel
	dg.ChannelMessageSend(m.ChannelID, "stopped music")
}

func findMembersChannel(guildID, userID string) string {
	guild, err := dg.State.Guild(guildID)
	if err != nil {
		return ""
	}
	for _, state := range guild.VoiceStates {
		if strings.EqualFold(userID, state.UserID) {
			return state.ChannelID
		}
	}
	return ""
}
