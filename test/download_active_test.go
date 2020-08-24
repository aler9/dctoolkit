package dctoolkit_test

import (
	"io/ioutil"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	dctk "github.com/aler9/dctoolkit"
)

func TestDownloadActive(t *testing.T) {
	foreachExternalHub(t, "DownloadActive", func(t *testing.T, e *externalHub) {
		ok := false

		client1 := func() {
			client, err := dctk.NewClient(dctk.ClientConf{
				LogLevel:           dctk.LogLevelError,
				HubUrl:             e.Url(),
				Nick:               "client1",
				Ip:                 dockerIp,
				TcpPort:            3006,
				UdpPort:            3006,
				PeerEncryptionMode: dctk.DisableEncryption,
				HubManualConnect:   true,
			})
			require.NoError(t, err)

			os.RemoveAll("/tmp/testshare")
			os.Mkdir("/tmp/testshare", 0755)
			ioutil.WriteFile("/tmp/testshare/test file.txt", []byte(strings.Repeat("A", 10000)), 0644)

			client.OnInitialized = func() {
				client.ShareAdd("share", "/tmp/testshare")
			}

			client.OnShareIndexed = func() {
				client.HubConnect()
			}

			client.Run()
		}

		client2 := func() {
			client, err := dctk.NewClient(dctk.ClientConf{
				LogLevel:           dctk.LogLevelError,
				HubUrl:             e.Url(),
				Nick:               "client2",
				Ip:                 dockerIp,
				TcpPort:            3005,
				UdpPort:            3005,
				PeerEncryptionMode: dctk.DisableEncryption,
			})
			require.NoError(t, err)

			client.OnHubConnected = func() {
				go client1()
			}

			client.OnPeerConnected = func(p *dctk.Peer) {
				if p.Nick == "client1" {
					client.DownloadFile(dctk.DownloadConf{
						Peer: p,
						TTH:  dctk.TigerHashMust("UJUIOGYVALWRB56PRJEB6ZH3G4OLTELOEQ3UKMY"),
					})
				}
			}

			client.OnDownloadSuccessful = func(d *dctk.Download) {
				ok = true
				client.Close()
			}

			client.Run()
		}

		client2()

		require.True(t, ok)
	})
}
