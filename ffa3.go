// Copyright 2021 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

// Package ffa3 implements support for the FlashForge Adventurer 3.
package ffa3

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"periph.io/x/conn/v3/physic"
)

// Position is a position in millimeter.
type Position struct {
	X physic.Distance
	Y physic.Distance
	Z physic.Distance
	_ struct{}
}

// Temperatures is the temperatures the printer can query or set.
type Temperatures struct {
	Extruder physic.Temperature
	Bed      physic.Temperature
	Chamber  physic.Temperature
	_        struct{}
}

// Info is the printer information as reported by itself.
type Info struct {
	Type          string
	Name          string
	Firmware      string
	Serial        string
	X             physic.Distance
	Y             physic.Distance
	Z             physic.Distance
	ExtruderCount int
	MacAddr       string
	_             struct{}
}

// Status is the printer status as reported by itself.
type Status struct {
	X        int
	Y        int
	Z        int
	Status   string
	MoveMode string
	Stuff    string
	_        struct{}
}

// Found is a printer found on the network.
type Found struct {
	IP   net.Addr
	Name string
	_    struct{}
}

func (f *Found) String() string {
	// TODO(maruel): Resolve IP address.
	return fmt.Sprintf("%s: %s", f.Name, f.IP)
}

// Search searches for printers via UDP discovery.
//
// It does so by sending bytes to a predetermined multicast IP address.
func SearchListenMulticast() ([]Found, error) {
	// Magic multicast IP the FlashForge Adventurer 3 is listening to.
	const ip = "225.0.0.9:19000"
	raddr, err := net.ResolveUDPAddr("udp4", ip)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve %s: %w", ip, err)
	}

	// The easiest to get the right UDP port to listen to multicast network is to
	// "dial" in UDP.
	conn, err := net.DialUDP("udp4", nil, raddr)
	if err != nil {
		return nil, err
	}
	laddr := conn.LocalAddr().(*net.UDPAddr)
	// Find a new port to listen to.
	laddr, err = net.ResolveUDPAddr("udp4", laddr.IP.String()+":0")
	if err != nil {
		return nil, err
	}
	laddr1 := conn.LocalAddr().(*net.UDPAddr)

	l, err := net.ListenMulticastUDP("udp4", nil, laddr)
	if err != nil {
		return nil, fmt.Errorf("failed listening to UDP: %w", err)
	}
	// Update the local address to get the port the listener is bound to.
	b := [8192]byte{}
	l.SetReadBuffer(len(b))
	var out []Found
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			n, src, err := l.ReadFromUDP(b[:])
			log.Printf("ReadFromUDP() = %v, %v, %v", n, src, err)
			if err != nil {
				// Ignore read errors since it'll fail when the connection is closed.
				break
			}
			//name := string(b[:n])
			name := hex.EncodeToString(b[:n])
			out = append(out, Found{IP: src, Name: name})
		}
	}()

	laddr2 := l.LocalAddr().(*net.UDPAddr)
	log.Printf("Listening on: %s:%d", laddr1.IP, laddr2.Port)
	magic := [8]byte{}
	copy(magic[:4], laddr1.IP)
	binary.LittleEndian.PutUint32(magic[4:], uint32(laddr2.Port))
	//copy(magic[:4], laddr.IP)
	//binary.LittleEndian.PutUint32(magic[4:], uint32(laddr.Port))
	log.Printf("Magic: %x", magic)
	if _, err := conn.Write(magic[:]); err != nil {
		log.Printf("err: %s", err)
		conn.Close()
		l.Close()
		wg.Wait()
		return nil, fmt.Errorf("failed to write magic packet: %w", err)
	}

	time.Sleep(time.Second)
	err = conn.Close()
	l.Close()
	wg.Wait()
	return out, err
}

// Search searches for printers via UDP discovery.
//
// It does so by sending bytes to a predetermined multicast IP address.
func SearchListen() ([]Found, error) {
	// Magic multicast IP the FlashForge Adventurer 3 is listening to.
	const ip = "225.0.0.9:19000"
	raddr, err := net.ResolveUDPAddr("udp4", ip)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve %s: %w", ip, err)
	}

	// The easiest to get the right UDP port to listen to multicast network is to
	// "dial" in UDP.
	conn, err := net.DialUDP("udp4", nil, raddr)
	if err != nil {
		return nil, err
	}
	laddr := conn.LocalAddr().(*net.UDPAddr)
	// Find a new port to listen to.
	laddr, err = net.ResolveUDPAddr("udp4", laddr.IP.String()+":0")
	if err != nil {
		return nil, err
	}

	l, err := net.ListenUDP("udp4", laddr)
	if err != nil {
		return nil, fmt.Errorf("failed listening to UDP: %w", err)
	}
	// Update the local address to get the port the listener is bound to.
	laddr = l.LocalAddr().(*net.UDPAddr)
	log.Printf("Listening on: %s", laddr)
	b := [8192]byte{}
	l.SetReadBuffer(len(b))
	var out []Found
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			n, src, err := l.ReadFromUDP(b[:])
			log.Printf("ReadFromUDP() = %v, %v, %v", n, src, err)
			if err != nil {
				// Ignore read errors since it'll fail when the connection is closed.
				break
			}
			out = append(out, Found{IP: src, Name: string(b[:n])})
		}
	}()

	magic := [8]byte{}
	copy(magic[:4], laddr.IP)
	binary.LittleEndian.PutUint32(magic[4:], uint32(laddr.Port))
	log.Printf("Magic: %x", magic)
	if _, err := conn.Write(magic[:]); err != nil {
		log.Printf("err: %s", err)
		conn.Close()
		wg.Wait()
		return nil, fmt.Errorf("failed to write magic packet: %w", err)
	}

	time.Sleep(time.Second)
	err = conn.Close()
	wg.Wait()
	return out, err
}

// Search searches for printers via UDP discovery.
//
// It does so by sending bytes to a predetermined multicast IP address.
func SearchNoListen() ([]Found, error) {
	// Magic multicast IP the FlashForge Adventurer 3 is listening to.
	const ip = "225.0.0.9:19000"
	raddr, err := net.ResolveUDPAddr("udp4", ip)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve %s: %w", ip, err)
	}

	// The easiest to get the right UDP port to listen to multicast network is to
	// "dial" in UDP.
	conn, err := net.DialUDP("udp4", nil, raddr)
	if err != nil {
		return nil, err
	}
	laddr := conn.LocalAddr().(*net.UDPAddr)
	log.Printf("Listening on: %v", laddr)
	b := [8192]byte{}
	conn.SetReadBuffer(len(b))
	var out []Found
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			n, src, err := conn.ReadFromUDP(b[:])
			log.Printf("ReadFromUDP() = %v, %v, %v", n, src, err)
			if err != nil {
				// Ignore read errors since it'll fail when the connection is closed.
				break
			}
			out = append(out, Found{IP: src, Name: string(b[:n])})
		}
	}()

	magic := [8]byte{}
	copy(magic[:4], laddr.IP)
	binary.LittleEndian.PutUint32(magic[4:], uint32(laddr.Port))
	log.Printf("Magic: %x", magic)
	if _, err := conn.Write(magic[:]); err != nil {
		log.Printf("err: %s", err)
		conn.Close()
		wg.Wait()
		return nil, fmt.Errorf("failed to write magic packet: %w", err)
	}

	time.Sleep(time.Second)
	conn.Close()
	wg.Wait()
	return out, nil
}

// Dev represents a FlashForge Adventurer 3 printer on the network.
//
// To retrieve a MJPEG stream from the printer's camera, connect to
// http://<ip>:8080/?action=stream.
type Dev struct {
	conn io.ReadWriteCloser
}

// Connect connects to the printer.
func Connect(ip string) (*Dev, error) {
	conn, err := net.Dial("tcp", ip+":8899")
	if err != nil {
		return nil, err
	}
	d := &Dev{conn: conn}
	if err := d.sendHello(); err != nil {
		d.Close()
		return nil, err
	}
	return d, nil
}

// Close closes the connection.
func (d *Dev) Close() error {
	err := d.sendBye()
	err2 := d.conn.Close()
	if err != nil {
		return err
	}
	return err2
}

// QueryPrinterInfo queries the printer information. This should never change so
// it can be safely cached.
func (d *Dev) QueryPrinterInfo(i *Info) error {
	resp, err := d.sendCommand("M115")
	if err != nil {
		return err
	}
	for _, line := range strings.Split(resp, "\n") {
		line = strings.TrimRight(line, "\r")
		switch {
		case strings.HasPrefix(line, "Machine Type: "):
			i.Type = line[len("Machine Type: "):]
		case strings.HasPrefix(line, "Machine Name: "):
			i.Name = line[len("Machine Name: "):]
		case strings.HasPrefix(line, "Firmware: "):
			i.Firmware = line[len("Firmware: "):]
		case strings.HasPrefix(line, "SN: "):
			i.Serial = line[len("SN: "):]
		case strings.HasPrefix(line, "X: "):
			r := regexp.MustCompile(`X: (\d+) Y: (\d+) Z: (\d+)`)
			if m := r.FindStringSubmatch(line); m != nil {
				v, err := strconv.Atoi(m[1])
				if err != nil {
					return err
				}
				i.X = physic.MilliMetre * physic.Distance(v)
				if v, err = strconv.Atoi(m[2]); err != nil {
					return err
				}
				i.Y = physic.MilliMetre * physic.Distance(v)
				if v, err = strconv.Atoi(m[3]); err != nil {
					return err
				}
				i.Z = physic.MilliMetre * physic.Distance(v)
				break
			}
			return fmt.Errorf("unknown M115 reply: %q", line)
		case strings.HasPrefix(line, "Tool Count: "):
			if i.ExtruderCount, err = strconv.Atoi(line[len("Tool Count: "):]); err != nil {
				return err
			}
		case strings.HasPrefix(line, "Mac Address: "):
			i.MacAddr = line[len("Mac Address: "):]
		case line == "CMD M115 Received.":
		case line == "ok":
		case line == "":
		default:
			return fmt.Errorf("unknown M115 reply: %q", line)
		}
	}
	return nil
}

// SetLight turns the printer's light on or off.
func (d *Dev) SetLight(on bool) error {
	// Channels must be lowercase. Duh.
	cmd := "M146 r0 g0 b0 F0"
	if on {
		cmd = "M146 r255 g255 b255 F0"
	}
	resp, err := d.sendCommand(cmd)
	if resp != "CMD M146 Received.\r\nok\r\n" {
		return fmt.Errorf("unknown M146 reply: %q", resp)
	}
	return err
}

// SetFan turns the printer's fan on or off.
func (d *Dev) SetFan(on bool) error {
	// TODO(maruel): It turns back on right after!
	cmd := "M107"
	if on {
		cmd = "M106 P1 S255"
	}
	_, err := d.sendCommand(cmd)
	return err
}

/*
// StopJob stops the running job.
func (d *Dev) StopJob() error {
	_, err := d.sendCommand("M26")
	return err
}

// PauseJob pauses the running job.
func (d *Dev) PauseJob() error {
	// S1 not needed?
	_, err := d.sendCommand("M601 S1")
	return err
}

// ResumeJob pauses the running job.
func (d *Dev) ResumeJob() error {
	_, err := d.sendCommand("M602")
	return err
}

// StopJob stops the running job but doesn't affect other parameters like
// heating.
func (d *Dev) StopJob() error {
	_, err := d.sendCommand("M603")
	return err
}

// FullStop stops everything right now.
func (d *Dev) FullStop() error {
	_, err := d.sendCommand("M112")
	return err
}

func (d *Dev) QueryStatus() error {
	_, err := d.sendCommand("M119")
	if err != nil {
		return err
	}
	return nil
}

// QueryTemp queries the temperatures.
func (d *Dev) QueryTemp(t *Temperatures) error {
	_, err := d.sendCommand("M105")
	if err != nil {
		return err
	}
	// "ok T:201 B:117" or more complicated.
	return nil
}

func (d *Dev) QueryJob() error {
	// M27 S2 reports every 2 seconds.
	_, err := d.sendCommand("M27")
	if err != nil {
		return err
	}
	// "Not SD printing."
	// "SD printing byte X/Y"
	return nil
}

func (d *Dev) QueryPosition() error {
	_, err := d.sendCommand("M114")
	if err != nil {
		return err
	}
	// "ok C: X:0.00 Y:0.00 Z:0.00 E:0.00"
	return nil
}
*/

// sendHello sends an hello command that must be the first command sent.
func (d *Dev) sendHello() error {
	resp, err := d.sendCommand("M601 S1")
	if err != nil {
		return err
	}
	if resp == "Control failed." {
		return errors.New("printer already has a connection; please disconnect other client first")
	}
	if resp != "Control Success." {
		return fmt.Errorf("failed to take control: %q", resp)
	}
	return nil
}

// sendBye sends a bye command that must be the last command sent.
func (d *Dev) sendBye() error {
	resp, err := d.sendCommand("M602")
	if err != nil {
		return err
	}
	if resp != "Control Release." {
		return fmt.Errorf("failed to release control: %q", resp)
	}
	return nil
}

// sendCommand sends a command, returns the trimmed response.
func (d *Dev) sendCommand(cmd string) (string, error) {
	// "~" is required, "\r\n" is not, "\n" is sufficient.
	if _, err := d.conn.Write([]byte("~" + cmd + "\n")); err != nil {
		log.Printf("sendCommand(%q): %s", cmd, err)
		return "", err
	}
	// TODO(maruel): Add timeout.
	resp := ""
	b := [4096]byte{}
	for {
		n, err := d.conn.Read(b[:])
		if resp += string(b[:n]); err != nil {
			log.Printf("sendCommand(%q): %q; %s", cmd, resp, err)
			return resp, err
		}
		if n != len(b) {
			break
		}
	}
	// Verify the reponse, it should be wrapped.
	c := strings.SplitN(cmd, " ", 2)[0]
	prefix := "CMD " + c + " Received.\r\n"
	if !strings.HasPrefix(resp, prefix) {
		return resp, fmt.Errorf("unknown %s reply: %q", c, resp)
	}
	if !strings.HasSuffix(resp, "\r\nok\r\n") {
		return resp, fmt.Errorf("unknown %s reply: %q", c, resp)
	}
	// Trim the wrap. Create a copy to not keep unneeded data in memory.
	line := string(resp[len(prefix) : len(resp)-len("ok\r\n")])
	log.Printf("sendCommand(%q): %q", cmd, line)
	return line, nil
}
