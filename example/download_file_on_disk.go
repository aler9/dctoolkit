package main

import (
    "io/ioutil"
    dctk "github.com/gswly/dctoolkit"
)

func main() {
    // automatically connect to hub. local ports must be opened and accessible (configure your router)
    client,err := dctk.NewClient(dctk.ClientConf{
        HubUrl: "nmdc://hubip:411",
        Nick: "mynick",
        TcpPort: 3009,
        UdpPort: 3009,
        TcpTlsPort: 3010,
    })
    if err != nil {
        panic(err)
    }

    // download a file by tth
    client.OnHubConnected = func() {
        client.Download(dctk.DownloadConf{
            Nick: "othernick",
            TTH: "AJ64KGNQ7OKNE7O4ARMYNWQ2VJF677BMUUQAR3Y",
        })
    }

    // download has finished: save the file on disk
    client.OnDownloadSuccessful = func(d *dctk.Download) {
        if err := ioutil.WriteFile("/path/to/outfile", d.Content(), 0655); err != nil {
            panic(err)
        }
    }

    client.Run()
}
