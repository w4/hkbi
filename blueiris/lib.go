package blueiris

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
)

type BlueirisConfig struct {
	Instance string
	Username string
	Password string
}

type Blueiris struct {
	mutex        *sync.RWMutex
	sessionToken string
	BaseUrl      *url.URL
	username     string
	password     string
}

func NewBlueiris(config BlueirisConfig) (*Blueiris, error) {
	base, err := url.Parse(config.Instance)
	if err != nil {
		return nil, err
	}

	bi := &Blueiris{
		mutex:        &sync.RWMutex{},
		sessionToken: "",
		BaseUrl:      base,
		username:     config.Username,
		password:     config.Password,
	}

	err = bi.login()
	if err != nil {
		return nil, err
	}

	return bi, nil
}

func (b *Blueiris) getSessionToken() string {
	cmd := struct {
		Cmd string `json:"cmd"`
	}{
		Cmd: "login",
	}

	var result struct {
		Session string `json:"session"`
	}

	_ = b.sendRequest(cmd, &result)

	return result.Session
}

func (b *Blueiris) login() error {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	b.sessionToken = b.getSessionToken()

	tokenHash := md5.Sum([]byte(fmt.Sprintf("%s:%s:%s", b.username, b.sessionToken, b.password)))
	token := hex.EncodeToString(tokenHash[:])

	cmd := struct {
		Cmd      string `json:"cmd"`
		Session  string `json:"session"`
		Response string `json:"response"`
	}{
		Cmd:      "login",
		Session:  b.sessionToken,
		Response: token,
	}

	var result struct {
		Result string `json:"result"`
	}

	err := b.sendRequest(cmd, &result)
	if err != nil {
		return err
	} else if result.Result != "success" {
		return errors.New("unsuccessful login")
	}

	return nil
}

type Camera struct {
	Id       string `json:"optionValue"`
	Name     string `json:"optionDisplay"`
	IsOnline bool   `json:"isOnline"`
	HasAudio bool   `json:"audio"`
	IsGroup  bool   `json:"group"`
	IsSystem bool   `json:"is_system"`
	Type     int    `json:"type"`
}

func (b *Blueiris) ListCameras() ([]Camera, error) {
	b.mutex.RLock()
	defer b.mutex.RUnlock()

	request := struct {
		Cmd     string `json:"cmd"`
		Session string `json:"session"`
	}{
		Cmd:     "camlist",
		Session: b.sessionToken,
	}

	var response struct {
		Data []Camera `json:"data"`
	}

	err := b.sendRequest(request, &response)
	if err != nil {
		return nil, err
	}

	return response.Data, nil
}

func (b *Blueiris) triggerCamera(camera string) error {
	b.mutex.RLock()
	defer b.mutex.RUnlock()

	request := struct {
		Cmd     string `json:"cmd"`
		Camera  string `json:"camera"`
		Session string `json:"session"`
	}{
		Cmd:     "trigger",
		Camera:  camera,
		Session: b.sessionToken,
	}

	response := struct{}{}

	err := b.sendRequest(request, &response)
	if err != nil {
		return err
	}

	return nil
}

func (b *Blueiris) FetchSnapshot(camera string) (*http.Request, error) {
	uri := b.BaseUrl.JoinPath("image", camera)
	uri.User = url.UserPassword(b.username, b.password)
	return http.NewRequest("GET", uri.String(), nil)
}

// TODO: try to renew session if result == 'fail'
func (b *Blueiris) sendRequest(request interface{}, res any) error {
	buf, err := json.Marshal(request)
	if err != nil {
		return err
	}

	reader := bytes.NewReader(buf)

	uri := b.BaseUrl.JoinPath("json")
	response, err := http.Post(uri.String(), "application/json", reader)
	if err != nil {
		return err
	}

	responseBytes, err := io.ReadAll(response.Body)
	if err != nil {
		return err
	}

	return json.Unmarshal(responseBytes, res)
}
