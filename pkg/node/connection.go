/*
Copyright © 2021-2022 Ettore Di Giacinto <mudler@mocaccino.org>
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package node

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"io"
	mrand "math/rand"
	"net"

	internalCrypto "github.com/mudler/edgevpn/pkg/crypto"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	conngater "github.com/libp2p/go-libp2p/p2p/net/conngater"
	hub "github.com/mudler/edgevpn/pkg/hub"
	multiaddr "github.com/multiformats/go-multiaddr"
)

// Host returns the libp2p peer host
func (e *Node) Host() host.Host {
	return e.host
}

// ConnectionGater returns the underlying libp2p conngater
func (e *Node) ConnectionGater() *conngater.BasicConnectionGater {
	return e.cg
}

// BlockSubnet blocks the CIDR subnet from connections
func (e *Node) BlockSubnet(cidr string) error {
	// Avoid to loopback traffic by trying to connect to nodes in via VPN
	_, n, err := net.ParseCIDR(cidr)
	if err != nil {
		return err
	}

	return e.ConnectionGater().BlockSubnet(n)
}

func (e *Node) genHost(ctx context.Context) (host.Host, error) {
	var r io.Reader
	if e.seed == 0 {
		r = rand.Reader
	} else {
		r = mrand.New(mrand.NewSource(e.seed))
	}

	prvKey, _, err := crypto.GenerateKeyPairWithReader(crypto.Ed25519, 4096, r)
	if err != nil {
		return nil, err
	}

	opts := e.config.Options

	cg, err := conngater.NewBasicConnectionGater(nil)
	if err != nil {
		return nil, err
	}

	e.cg = cg

	if e.config.InterfaceAddress != "" {
		e.BlockSubnet(e.config.InterfaceAddress)
	}

	for _, b := range e.config.Blacklist {
		_, net, err := net.ParseCIDR(b)
		if err != nil {
			// Assume it's a peerID
			cg.BlockPeer(peer.ID(b))
		}
		if net != nil {
			cg.BlockSubnet(net)
		}
	}

	opts = append(opts, libp2p.ConnectionGater(cg), libp2p.Identity(prvKey))

	addrs := []multiaddr.Multiaddr{}
	for _, l := range e.config.ListenAddresses {
		addrs = append(addrs, []multiaddr.Multiaddr(l)...)
	}
	opts = append(opts, libp2p.ListenAddrs(addrs...))

	for _, d := range e.config.ServiceDiscovery {
		opts = append(opts, d.Option(ctx))
	}

	opts = append(opts, e.config.AdditionalOptions...)

	if e.config.Insecure {
		e.config.Logger.Info("Disabling Security transport layer")
		opts = append(opts, libp2p.NoSecurity)
	}

	opts = append(opts, libp2p.FallbackDefaults)

	return libp2p.New(opts...)
}

func (e *Node) sealkey() string {
	return internalCrypto.MD5(internalCrypto.TOTP(sha256.New, e.config.SealKeyLength, e.config.SealKeyInterval, e.config.ExchangeKey))
}

func (e *Node) handleEvents(ctx context.Context, inputChannel chan *hub.Message, roomMessages chan *hub.Message, pub func(*hub.Message) error, handlers []Handler, peerGater bool) {
	for {
		select {
		case m := <-inputChannel:
			if m == nil {
				continue
			}
			c := m.Copy()
			str, err := e.config.Sealer.Seal(c.Message, e.sealkey())
			if err != nil {
				e.config.Logger.Warnf("%w from %s", err.Error(), c.SenderID)
			}
			c.Message = str

			if err := pub(c); err != nil {
				e.config.Logger.Warnf("publish error: %s", err)
			}

		case m := <-roomMessages:
			if m == nil {
				continue
			}

			if peerGater {
				if e.config.PeerGater != nil && e.config.PeerGater.Gate(e, peer.ID(m.SenderID)) {
					e.config.Logger.Warnf("gated message from %s", m.SenderID)
					continue
				}
			}

			c := m.Copy()
			str, err := e.config.Sealer.Unseal(c.Message, e.sealkey())
			if err != nil {
				e.config.Logger.Warnf("%w from %s", err.Error(), c.SenderID)
			}
			c.Message = str
			e.handleReceivedMessage(c, handlers, inputChannel)
		case <-ctx.Done():
			return
		}
	}
}

func (e *Node) handleReceivedMessage(m *hub.Message, handlers []Handler, c chan *hub.Message) {
	for _, h := range handlers {
		if err := h(e.ledger, m, c); err != nil {
			e.config.Logger.Warnf("handler error: %s", err)
		}
	}
}
