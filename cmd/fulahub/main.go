package main

import (
	"context"
	"encoding/base64"
	"flag"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/functionland/fulahub"
	leveldb "github.com/ipfs/go-ds-leveldb"
	"github.com/ipfs/go-log/v2"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/crypto"
)

var logger = log.Logger("fula/hub/main")

func main() {

	if _, ok := os.LookupEnv("GOLOG_LOG_LEVEL"); !ok {
		_ = log.SetLogLevel("*", "info")
	}

	identity := flag.String("identity", "", "base64 encoded libp2p identity")
	listenAddrs := flag.String("listenAddrs",
		"/ip4/0.0.0.0/tcp/40004,/ip4/0.0.0.0/udp/40004/quic-v1,/ip4/0.0.0.0/udp/40004/quic-v1/webtransport", "comma separated libp2p listen addresses")
	announceAddrs := flag.String("announceAddrs",
		"", "comma separated IPNI announce addresses")
	providerAddrs := flag.String("providerAddrs",
		"", "comma separated IPNI provider addresses")
	dsPath := flag.String("dsPath",
		"", "Datastore path")

	flag.Parse()

	var lopts []libp2p.Option
	if *identity != "" {
		decodeString, err := base64.StdEncoding.DecodeString(*identity)
		if err != nil {
			panic(err)
		}
		key, err := crypto.UnmarshalPrivateKey(decodeString)
		if err != nil {
			panic(err)
		}
		lopts = append(lopts, libp2p.Identity(key))
	}
	if *listenAddrs != "" {
		lopts = append(lopts, libp2p.ListenAddrStrings(strings.Split(*listenAddrs, ",")...))
	}

	host, err := libp2p.New(lopts...)
	if err != nil {
		panic(err)
	}

	hopts := []fulahub.Option{
		fulahub.WithHost(host),
	}
	if *providerAddrs != "" {
		hopts = append(hopts, fulahub.WithIpniProviderAddrsString(strings.Split(*providerAddrs, ",")...))
	}
	if *announceAddrs != "" {
		hopts = append(hopts, fulahub.WithIpniAnnounceAddrsString(strings.Split(*announceAddrs, ",")...))
	}
	if *dsPath != "" {
		ds, err := leveldb.NewDatastore(*dsPath, nil)
		if err != nil {
			panic(err)
		}
		hopts = append(hopts, fulahub.WithDatastore(ds))
	}
	hub, err := fulahub.NewHub(hopts...)
	if err != nil {
		panic(err)
	}
	ctx := context.Background()
	if err := hub.Start(ctx); err != nil {
		panic(err)
	}
	logger.Info("Started fula hub", "pid", host.ID())

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	if err := hub.Shutdown(ctx); err != nil {
		logger.Errorw("Failed to shut down fula hub gracefully", "err", err)
	}
}
