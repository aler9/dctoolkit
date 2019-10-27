package dctoolkit_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	dctk "github.com/aler9/dctoolkit"
)

func TestConnPwd(t *testing.T) {
	foreachExternalHub(t, func(t *testing.T, e *externalHub) {
		ok := false
		dctk.SetLogLevel(dctk.LevelError)

		client, err := dctk.NewClient(dctk.ClientConf{
			HubUrl:     e.Url(),
			Nick:       "testdctk_auth",
			Password:   "testpa$ss",
			Ip:         "127.0.0.1",
			TcpPort:    3006,
			UdpPort:    3006,
			TcpTlsPort: 3007,
		})
		require.NoError(t, err)

		client.OnHubConnected = func() {
			go func() {
				time.Sleep(1 * time.Second)
				client.Safe(func() {
					ok = true
					client.Close()
				})
			}()
		}

		client.Run()
		require.True(t, ok)
	})
}