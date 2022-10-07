package main

import (
	"context"
	"encoding/gob"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/BurntSushi/toml"
	"github.com/brutella/hap"
	"github.com/brutella/hap/accessory"
	"github.com/brutella/hap/characteristic"
	"github.com/brutella/hap/log"
	"github.com/brutella/hap/rtp"
	"github.com/brutella/hap/service"
	"github.com/brutella/hap/tlv8"
	"github.com/w4/hkbi/blueiris"
	service2 "github.com/w4/hkbi/service"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
)

type Config struct {
	ListenAddress string `toml:"listen-address"`
	DataDir       string `toml:"data-dir"`
	Blueiris      blueiris.BlueirisConfig
}

type GlobalState struct {
	ssrcVideo int32
	ssrcAudio int32
}

func main() {
	log.Info.Enable()
	log.Debug.Enable()

	if len(os.Args) != 2 || os.Args[1] == "" {
		log.Info.Fatalf("usage: %s /path/to/config.toml", os.Args[0])
	}

	var config = readConfig(os.Args[1])
	run(config)
}

func readConfig(path string) Config {
	var cfg Config
	_, err := toml.DecodeFile(path, &cfg)
	if err != nil {
		log.Info.Panic(err)
	}

	return cfg
}

func run(config Config) {
	bi, err := blueiris.NewBlueiris(config.Blueiris)
	if err != nil {
		log.Info.Fatalf("failed to login to bi: %s\n", err)
	}

	globalState := &GlobalState{
		ssrcVideo: rand.Int31(),
		ssrcAudio: rand.Int31(),
	}

	// fetch cameras from BlueIris
	biCameras, err := bi.ListCameras()
	if err != nil {
		log.Info.Fatalf("failed to load cameras from bi: %s\n", err)
	}

	err = os.MkdirAll(config.DataDir, os.FileMode(0755))
	if err != nil {
		log.Info.Fatalf("failed to create data directory: %s\n", err)
	}

	knownCamerasPath := filepath.Join(config.DataDir, "knownCameras")

	// read our known camera list from disk for stable ids
	var knownCameras = make(map[string]int)
	file, err := os.Open(knownCamerasPath)
	if err != nil {
		// if not exists, we'll just ignore the error and default to the empty list
		if !os.IsNotExist(err) {
			log.Info.Fatalf("failed to read knownCameras: %s\n", err)
		}
	} else {
		err = gob.NewDecoder(file).Decode(&knownCameras)
		if err != nil {
			log.Info.Fatalf("failed to decode knownCameras: %s\n", err)
		}
	}
	_ = file.Close()

	hasDiscoveredNewCameras := false

	// create HomeKit cameras and motion sensors from the fetched BlueIris cameras
	cameras := make([]*accessory.Camera, 0, len(biCameras))
	motionSensors := make(map[string]*service.MotionSensor)
	for _, camera := range biCameras {
		// create the HomeKit camera accessory
		cam := accessory.NewCamera(accessory.Info{
			Name:         camera.Name,
			Manufacturer: "HKBI",
		})

		if id, exists := knownCameras[camera.Name]; exists {
			log.Debug.Printf("reusing previously assigned id %d for camera %s", id, camera.Name)
			cam.Id = uint64(id)
		} else {
			newId := 0
			for _, i := range knownCameras {
				if i >= newId {
					newId = i + 1
				}
			}

			log.Info.Printf("newly discovered camera %s assigned id %d", camera.Name, newId)
			knownCameras[camera.Name] = newId
			cam.Id = uint64(newId)

			hasDiscoveredNewCameras = true
		}

		// setup stream request handling on channel 1
		startListeningForStreams(camera.Id, cam.StreamManagement1, globalState, &config, bi.BaseUrl)

		// create the camera operating mode service
		cameraOperatingMode := service2.NewCameraOperatingMode()
		cam.AddS(cameraOperatingMode.S)

		// create the HomeKit motion sensor service
		motionSensor := service.NewMotionSensor()
		motionSensorActive := characteristic.NewActive()
		motionSensor.AddC(motionSensorActive.C)

		// create camera recording management service
		recordingManagement := service.NewCameraRecordingManagement()
		cam.AddS(recordingManagement.S)

		// add motion sensor service to camera - TODO: needs to add to DataStreamManagement too
		cam.AddS(motionSensor.S)

		// add the cameras to our output array/map for adding to the server and dispatching
		// events to
		cameras = append(cameras, cam)
		motionSensors[camera.Id] = motionSensor
	}

	// write newly discovered cameras to disk
	if hasDiscoveredNewCameras {
		file, err = os.Create(knownCamerasPath)
		if err != nil {
			log.Info.Fatalf("failed to create knownCameras: %s\n", err)
		}

		err = gob.NewEncoder(file).Encode(knownCameras)
		if err != nil {
			log.Info.Fatalf("failed to encode knownCameras: %s\n", err)
		}

		_ = file.Close()
	}

	// fetch all the created accessories for exposing to HomeKit
	var accessories = make([]*accessory.A, 0, len(cameras))
	for _, camera := range cameras {
		accessories = append(accessories, camera.A)
	}

	// setup hap's state storage
	fs := hap.NewFsStore(config.DataDir)

	// start building the homekit accessory protocol (hap) server
	server, err := hap.NewServer(fs, accessories[0], accessories[1:]...)
	if err != nil {
		log.Info.Panic(err)
	}

	// set our HAP config
	server.Pin = "11111112"
	server.Addr = config.ListenAddress

	// endpoint to trigger a camera's motion sensor for 10 seconds
	server.ServeMux().HandleFunc("/trigger", func(res http.ResponseWriter, req *http.Request) {
		query := req.URL.Query()
		state := query.Get("state")
		cam := query.Get("cam")

		if sensor := motionSensors[cam]; sensor != nil {
			var motionDetected bool
			if state == "off" {
				motionDetected = false
			} else {
				motionDetected = true
			}

			sensor.MotionDetected.SetValue(motionDetected)

			res.WriteHeader(http.StatusOK)
		} else {
			log.Info.Printf("Received trigger request for unknown camera: %s", cam)
			res.WriteHeader(http.StatusBadRequest)
		}
	})

	// endpoint to handle snapshot requests from HomeKit
	server.ServeMux().HandleFunc("/resource", func(res http.ResponseWriter, req *http.Request) {
		var request struct {
			Type string `json:"resource-type"`
			Aid  int    `json:"aid"`
		}

		// ensure this is a valid resource request
		if !server.IsAuthorized(req) {
			_ = hap.JsonError(res, hap.JsonStatusInsufficientPrivileges)
			return
		} else if req.Method != http.MethodPost {
			res.WriteHeader(http.StatusBadRequest)
			return
		}

		// read request body
		body, err := io.ReadAll(req.Body)
		if err != nil {
			log.Info.Println(err)
			res.WriteHeader(http.StatusInternalServerError)
			return
		}

		// parse request body
		err = json.Unmarshal(body, &request)
		if err != nil {
			log.Info.Println(err)
			res.WriteHeader(http.StatusBadRequest)
			return
		}

		var cameraName string
		for name, i := range knownCameras {
			if i == request.Aid {
				cameraName = name
				break
			}
		}

		if cameraName == "" {
			log.Info.Printf("a snapshot was requested for camera %d but not camera with that id exists", request.Aid)
			res.WriteHeader(http.StatusBadRequest)
			return
		}

		switch request.Type {
		case "image":
			// build request to fetch a snapshot of the camera from blueiris
			req, err := bi.FetchSnapshot(cameraName)
			if err != nil {
				log.Info.Println(err)
				res.WriteHeader(http.StatusInternalServerError)
				return
			}

			// send request to blueiris
			imageResponse, err := http.DefaultClient.Do(req)
			if err != nil {
				log.Info.Println(err)
				res.WriteHeader(http.StatusInternalServerError)
				return
			}
			defer func(Body io.ReadCloser) {
				_ = Body.Close()
			}(imageResponse.Body)

			// set response headers
			res.Header().Set("Content-Type", "image/jpeg")

			// stream response from blueiris to HomeKit
			wr := hap.NewChunkedWriter(res, 2048)
			_, err = io.Copy(wr, imageResponse.Body)
			if err != nil {
				log.Info.Printf("Failed to copy bytes for snapshot to HomeKit: %s\n", err)
				return
			}
		default:
			log.Info.Printf("unsupported resource request \"%s\"\n", request.Type)
			res.WriteHeader(http.StatusInternalServerError)
			return
		}
	})

	// set up a listener for sigint and sigterm signals to stop the server
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	signal.Notify(c, syscall.SIGTERM)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-c
		signal.Stop(c)
		cancel()
	}()

	// spawn the server
	err = server.ListenAndServe(ctx)
	if err != nil {
		log.Info.Panic(err)
	}
}

// sets up a camera accessory for streaming
func startListeningForStreams(cameraName string, mgmt *service.CameraRTPStreamManagement, globalState *GlobalState, config *Config, blueirisBase *url.URL) {
	// add active characteristic to rtpstream
	active := characteristic.NewActive()
	mgmt.AddC(active.C)

	// set up some basic parameters for HomeKit to know that the camera is available
	setTlv8Payload(mgmt.StreamingStatus.Bytes, rtp.StreamingStatus{Status: rtp.StreamingStatusAvailable})
	setTlv8Payload(mgmt.SupportedRTPConfiguration.Bytes, rtp.NewConfiguration(rtp.CryptoSuite_AES_CM_128_HMAC_SHA1_80))
	setTlv8Payload(mgmt.SupportedVideoStreamConfiguration.Bytes, rtp.DefaultVideoStreamConfiguration())
	setTlv8Payload(mgmt.SupportedAudioStreamConfiguration.Bytes, rtp.DefaultAudioStreamConfiguration())

	// shared state for all the spawned streams, with a mapping to the session id for us to
	// figure out which stream is being referred to
	var activeStreams = ActiveStreams{
		mutex:   &sync.Mutex{},
		streams: map[string]*Stream{},
	}

	// handle the initial request sent to us from HomeKit to set up a new stream
	mgmt.SetupEndpoints.OnValueUpdate(func(new, old []byte, r *http.Request) {
		// HomeKit ends up sending us two requests, but the second one doesn't have a http request attached,
		// so we can just ignore it
		if r == nil {
			return
		}

		var req rtp.SetupEndpoints

		// unmarshal request from HomeKit
		err := tlv8.Unmarshal(new, &req)
		if err != nil {
			log.Info.Printf("Could not unmarshal tlv8 data: %s\n", err)
			return
		}

		// encode the session id, so it's human-readable for logging
		var uuid = hex.EncodeToString(req.SessionId)

		// build the response to send back to HomeKit
		resp := rtp.SetupEndpointsResponse{
			SessionId: req.SessionId,
			Status:    rtp.SessionStatusSuccess,
			AccessoryAddr: rtp.Addr{
				IPVersion:    req.ControllerAddr.IPVersion,
				IPAddr:       strings.Split(r.Context().Value(http.LocalAddrContextKey).(net.Addr).String(), ":")[0],
				VideoRtpPort: req.ControllerAddr.VideoRtpPort,
				AudioRtpPort: req.ControllerAddr.AudioRtpPort,
			},
			Video:     req.Video,
			Audio:     req.Audio,
			SsrcVideo: globalState.ssrcVideo,
			SsrcAudio: globalState.ssrcAudio,
		}

		// create and track the new stream
		activeStreams.mutex.Lock()
		activeStreams.streams[uuid] = &Stream{
			mutex: &sync.Mutex{},
			cmd:   nil,
			req:   req,
			resp:  resp,
		}
		activeStreams.mutex.Unlock()

		// send the response to HomeKit
		setTlv8Payload(mgmt.SetupEndpoints.Bytes, resp)
	})

	// handle streaming requests from HomeKit
	mgmt.SelectedRTPStreamConfiguration.OnValueRemoteUpdate(func(buf []byte) {
		var cfg rtp.StreamConfiguration

		// unmarshal request from HomeKit
		err := tlv8.Unmarshal(buf, &cfg)
		if err != nil {
			log.Info.Fatalf("Could not unmarshal tlv8 data: %s\n", err)
		}

		// encode the session id, so it's human-readable for logging
		uuid := hex.EncodeToString(cfg.Command.Identifier)

		// match the command that HomeKit wants to perform for the stream uuid
		switch cfg.Command.Type {
		case rtp.SessionControlCommandTypeStart:
			stream := activeStreams.streams[uuid]
			if stream == nil {
				return
			}

			log.Info.Printf("%s: starting stream\n", uuid)

			// lock the stream, so we're not racing with another request to spawn an ffmpeg instance
			// and update the state
			stream.mutex.Lock()
			defer stream.mutex.Unlock()

			// close any previous ffmpeg instances that were open for the given stream uuid
			if stream.cmd != nil && stream.cmd.Process != nil {
				log.Info.Printf("%s: requested to start stream, but stream was already running. shutting down previous\n", uuid)

				_ = stream.cmd.Process.Signal(syscall.SIGINT)
				status, _ := stream.cmd.Process.Wait()
				log.Info.Printf("%s: ffmpeg exited with %s\n", uuid, status.String())

				stream.cmd = nil
			}

			// build the endpoint that HomeKit wants us to stream to
			endpoint := fmt.Sprintf(
				"srtp://%s:%d?rtcpport=%d&pkt_size=%d",
				stream.req.ControllerAddr.IPAddr,
				stream.req.ControllerAddr.VideoRtpPort,
				stream.req.ControllerAddr.VideoRtpPort,
				1378,
			)

			// build the blueiris rtsp source
			source := blueirisBase.JoinPath(cameraName)
			source.Scheme = "rtsp"
			source.User = url.UserPassword(config.Blueiris.Username, config.Blueiris.Password)

			// build ffmpeg command for pulling RTSP stream from BlueIris and forwarding to the HomeKit
			// controller's SRTP port using pass-through for low CPU, the BlueIris RTSP web server needs
			// to be set to 2,000kb/s bitrate though otherwise iOS will silently fail
			cmd := exec.Command(
				"ffmpeg",
				// input
				"-an",
				"-rtsp_transport", "tcp",
				"-use_wallclock_as_timestamps", "1",
				"-i", source.String(),
				// no audio
				"-an",
				// no subs
				"-sn",
				// no data
				"-dn",
				// add extra keyframes, so we don't need to worry about the blueiris settings
				"-bsf:v", "dump_extra",
				// copy data directly from the blueiris stream
				"-vcodec", "copy",
				// requested payload type from client
				"-payload_type", fmt.Sprintf("%d", cfg.Video.RTP.PayloadType),
				// sync source
				"-ssrc", fmt.Sprintf("%d", globalState.ssrcVideo),
				// format rtp
				"-f", "rtp",
				// forward over srtp to the controller
				"-srtp_out_suite", "AES_CM_128_HMAC_SHA1_80",
				"-srtp_out_params", stream.req.Video.SrtpKey(),
				endpoint,
			)

			// forward ffmpeg to console
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			log.Debug.Println(cmd)

			// spawn ffmpeg command
			err := cmd.Start()
			if err != nil {
				log.Info.Printf("Failed to spawn ffmpeg: %s\n", err)
				return
			}

			// update our state to contain the spawned command, so we can control it later
			stream.cmd = cmd

			// sanity check to ensure our status is still available so new clients can still request
			// streams
			setTlv8Payload(mgmt.StreamingStatus.Bytes, rtp.StreamingStatus{Status: rtp.StreamingStatusAvailable})
		case rtp.SessionControlCommandTypeEnd:
			stream := activeStreams.streams[uuid]
			if stream == nil {
				return
			}

			log.Info.Printf("%s: ending stream\n", uuid)

			// lock the stream, so we're not racing with another request on the process and update
			// the state
			stream.mutex.Lock()
			defer stream.mutex.Unlock()

			// ensure the stream is still open
			if stream.cmd == nil || stream.cmd.Process == nil {
				log.Info.Printf("%s: attempted to end already ended stream\n", uuid)
				return
			}

			// send a sigint to ffmpeg and wait for it to finish
			_ = stream.cmd.Process.Signal(syscall.SIGINT)
			status, _ := stream.cmd.Process.Wait()
			log.Info.Printf("%s: ffmpeg exited with %s\n", uuid, status.String())

			// remove command from our state so HomeKit can't attempt to close it twice
			stream.cmd = nil

			// sanity check to ensure our status is still available so new clients can still request
			// streams
			setTlv8Payload(mgmt.StreamingStatus.Bytes, rtp.StreamingStatus{Status: rtp.StreamingStatusAvailable})
		case rtp.SessionControlCommandTypeSuspend:
			stream := activeStreams.streams[uuid]
			if stream == nil {
				return
			}

			log.Info.Printf("%s: suspending stream\n", uuid)

			// lock the stream, so we're not racing with another request on the process
			stream.mutex.Lock()
			defer stream.mutex.Unlock()

			// ensure HomeKit isn't attempting to suspend a closed stream
			if stream.cmd == nil || stream.cmd.Process == nil {
				log.Info.Printf("%s: attempted to suspend inactive stream\n", uuid)
				return
			}

			// send a sigstop signal to ffmpeg
			err := stream.cmd.Process.Signal(syscall.SIGSTOP)
			if err != nil {
				log.Info.Printf("%s: failed to suspend ffmpeg: %s\n", uuid, err)
			}
		case rtp.SessionControlCommandTypeResume:
			stream := activeStreams.streams[uuid]
			if stream == nil {
				return
			}

			log.Info.Printf("%s: resuming stream\n", uuid)

			// lock the stream, so we're not racing with another request on the process
			stream.mutex.Lock()
			defer stream.mutex.Unlock()

			// ensure HomeKit isn't attempting to resume a closed stream
			if stream.cmd == nil || stream.cmd.Process == nil {
				log.Info.Printf("%s: attempted to resume inactive stream\n", uuid)
				return
			}

			// send a sigcont signal to ffmpeg
			err := stream.cmd.Process.Signal(syscall.SIGCONT)
			if err != nil {
				log.Info.Printf("%s: failed to resume ffmpeg: %s\n", uuid, err)
			}
		case rtp.SessionControlCommandTypeReconfigure:
			log.Info.Printf("%s: ignoring reconfigure message\n", uuid)
		default:
			log.Debug.Printf("%s: Unknown command type %d\n", uuid, cfg.Command.Type)
		}
	})
}

func setTlv8Payload(c *characteristic.Bytes, v interface{}) {
	if val, err := tlv8.Marshal(v); err == nil {
		c.SetValue(val)
	} else {
		log.Info.Println(err)
	}
}

type Stream struct {
	mutex *sync.Mutex
	cmd   *exec.Cmd
	req   rtp.SetupEndpoints
	resp  rtp.SetupEndpointsResponse
}

type ActiveStreams struct {
	mutex   *sync.Mutex
	streams map[string]*Stream
}
