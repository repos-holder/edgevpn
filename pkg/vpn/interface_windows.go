//go:build windows
// +build windows

// Copyright © 2021 Ettore Di Giacinto <mudler@mocaccino.org>
//
// This program is free software; you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation; either version 2 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License along
// with this program; if not, see <http://www.gnu.org/licenses/>.

package vpn

import (
	"fmt"
	"log"
	"os/exec"

	"github.com/fumiama/water"
	"github.com/fumiama/wintun"
	"golang.org/x/sys/windows"
	"github.com/google/uuid"
)

func prepareInterface(c *Config) error {
	err := netsh("interface", "ip", "set", "address", "name=", c.InterfaceName, "static", c.InterfaceAddress)
	if err != nil {
		log.Println(err)
	}
	err = netsh("interface", "ipv4", "set", "subinterface", c.InterfaceName, "mtu=", fmt.Sprintf("%d", c.InterfaceMTU))
	if err != nil {
		log.Println(err)
	}
	return nil
}

func createInterface(c *Config) (*water.Interface, error) {
	// Use deterministic GUID based on interface name, so we
	// don't flood system with Network Profiles
	uuid, _ := uuid.FromBytes([]byte(c.InterfaceName))
	guid, _ := windows.GUIDFromString("{" + uuid.String() + "}")
	// Create an adapter with deterministic GUID which water will
	// take via wintun.OpenAdapter()
	wintun.CreateAdapter(c.InterfaceName, "WaterWintun", &guid)
	config := water.Config{
		DeviceType: c.DeviceType,
		PlatformSpecificParams: water.PlatformSpecificParams{
			ComponentID: "WaterWintun",
			InterfaceName: c.InterfaceName,
		},
	}

	return water.New(config)
}

func netsh(args ...string) (err error) {
	cmd := exec.Command("netsh", args...)
	err = cmd.Run()
	return
}
