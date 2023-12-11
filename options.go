package fulahub

import (
	"bytes"
	"io"
	"net/url"

	"github.com/ipfs/go-datastore"
	"github.com/ipfs/go-datastore/namespace"
	"github.com/ipfs/go-datastore/sync"
	"github.com/ipld/go-ipld-prime"
	"github.com/ipld/go-ipld-prime/datamodel"
	"github.com/ipld/go-ipld-prime/linking"
	cidlink "github.com/ipld/go-ipld-prime/linking/cid"
	"github.com/ipni/go-libipni/announce"
	"github.com/ipni/go-libipni/announce/httpsender"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/multiformats/go-multiaddr"
)

type (
	Option  func(*options) error
	options struct {
		h                  host.Host
		ls                 *ipld.LinkSystem
		ipniProviderAddrs  func(host.Host) []string
		ipniAnnounceAddrs  func(host.Host) []multiaddr.Multiaddr
		ipniAnnounceSender announce.Sender
		ds                 datastore.Datastore
	}
)

func newOptions(o ...Option) (*options, error) {
	opts := options{
		ipniProviderAddrs: func(h host.Host) []string {
			addrs := h.Addrs()
			saddrs := make([]string, 0, len(addrs))
			for _, addr := range addrs {
				saddrs = append(saddrs, addr.String())
			}
			return saddrs
		},
		ipniAnnounceAddrs: func(h host.Host) []multiaddr.Multiaddr {
			return h.Addrs()
		},
	}
	for _, apply := range o {
		if err := apply(&opts); err != nil {
			return nil, err
		}
	}

	var err error
	if opts.ds == nil {
		opts.ds = sync.MutexWrap(datastore.NewMapDatastore())
	}
	if opts.h == nil {
		opts.h, err = libp2p.New()
		if err != nil {
			return nil, err
		}
	}

	if opts.ipniAnnounceSender == nil {
		aurl, err := url.Parse("https://cid.contact/ingest/announce")
		if err != nil {
			return nil, err
		}
		opts.ipniAnnounceSender, err = httpsender.New([]*url.URL{aurl}, opts.h.ID())
	}

	if opts.ls == nil {
		ls := cidlink.DefaultLinkSystem()
		opts.ls = &ls
		lsds := namespace.Wrap(opts.ds, datastore.NewKey("ls"))
		opts.ls.StorageReadOpener = func(lctx linking.LinkContext, link datamodel.Link) (io.Reader, error) {
			value, err := lsds.Get(lctx.Ctx, datastore.NewKey(link.Binary()))
			if err != nil {
				return nil, err
			}
			return bytes.NewBuffer(value), nil
		}
		opts.ls.StorageWriteOpener = func(lctx linking.LinkContext) (io.Writer, linking.BlockWriteCommitter, error) {
			var buf bytes.Buffer
			return &buf, func(lnk ipld.Link) error {
				return lsds.Put(lctx.Ctx, datastore.NewKey(lnk.Binary()), buf.Bytes())
			}, nil
		}
	}

	return &opts, nil
}

func WithHost(h host.Host) Option {
	return func(o *options) error {
		o.h = h
		return nil
	}
}

func WithDatastore(ds datastore.Datastore) Option {
	return func(o *options) error {
		o.ds = ds
		return nil
	}
}

func WithIpniProviderAddrsString(saddr ...string) Option {
	return func(o *options) error {
		o.ipniProviderAddrs = func(host.Host) []string { return saddr }
		return nil
	}
}

func WithIpniAnnounceAddrsString(saddr ...string) Option {
	return func(o *options) error {
		addrs := make([]multiaddr.Multiaddr, 0, len(saddr))
		for _, s := range saddr {
			a, err := multiaddr.NewMultiaddr(s)
			if err != nil {
				return err
			}
			addrs = append(addrs, a)
		}
		o.ipniAnnounceAddrs = func(host.Host) []multiaddr.Multiaddr {
			return addrs
		}
		return nil
	}
}
