package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/stretchr/testify/assert"
)

func TestServeHTTP(t *testing.T) {
	assert := assert.New(t)
	plugin := Plugin{}
	api := &plugintest.API{}
	plugin.SetAPI(api)

	t.Run("get default meeting settings", func(t *testing.T) {
		// Mock get default meeting
		defaultMeeting := &Meeting{
			ChannelID:     "myChannelId",
			Schedule:      []time.Weekday{time.Thursday},
			HashtagFormat: "Jan02",
		}

		jsonMeeting, err := json.Marshal(defaultMeeting)
		assert.Nil(err)

		api.On("KVGet", "myChannelId").Return(jsonMeeting, nil)

		r := httptest.NewRequest(http.MethodGet, "/api/v1/settings?channelId=myChannelId", nil)
		r.Header.Add("Mattermost-User-Id", "theuserid")

		w := httptest.NewRecorder()
		plugin.ServeHTTP(nil, w, r)

		result := w.Result()
		assert.NotNil(result)
		bodyBytes, err := io.ReadAll(result.Body)
		assert.Nil(err)

		assert.Equal(string(jsonMeeting), string(bodyBytes))
	})

	t.Run("post meeting settings", func(t *testing.T) {
		// Mock set meeting
		meeting := &Meeting{
			ChannelID:     "myChannelId",
			Schedule:      []time.Weekday{time.Tuesday},
			HashtagFormat: "MyMeeting-Jan-02",
		}

		jsonMeeting, err := json.Marshal(meeting)
		assert.Nil(err)

		api.On("KVSet", "myChannelId", jsonMeeting).Return(nil)

		r := httptest.NewRequest(http.MethodPost, "/api/v1/settings", strings.NewReader(string(jsonMeeting)))
		r.Header.Add("Mattermost-User-Id", "theuserid")

		w := httptest.NewRecorder()
		plugin.ServeHTTP(nil, w, r)

		result := w.Result()
		assert.NotNil(result)
		assert.Equal(http.StatusOK, result.StatusCode)
	})
}
