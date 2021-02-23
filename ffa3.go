// Copyright 2021 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

// Package ffa3 implements support for the FlashForge Adventurer 3.
package ffa3

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"

	"periph.io/x/conn/v3/physic"
)

// Position is a position in millimeter.
type Position struct {
	X physic.Distance
	Y physic.Distance
	Z physic.Distance
	A int
	B int
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
	IP   net.IP
	Name string
	_    struct{}
}

func (f *Found) String() string {
	return fmt.Sprintf("%s (%s)", f.Name, f.IP)
}

// Search searches for printers via UDP discovery.
//
// It does so by sending bytes to a predetermined multicast IP address.
func Search(first bool, d time.Duration) ([]Found, error) {
	// Magic multicast IP the FlashForge Adventurer 3 is listening to.
	const ip = "225.0.0.9:19000"
	raddr, err := net.ResolveUDPAddr("udp4", ip)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve %s: %w", ip, err)
	}

	// In practice we'd want to specify the right IP here, because otherwise
	// laddr is set to 0.0.0.0. In practice it seems to work anyway.
	// May want to revisit later.
	l, err := net.ListenUDP("udp4", nil)
	if err != nil {
		return nil, fmt.Errorf("failed listening to UDP: %w", err)
	}
	// Update the local address to get the port the listener is bound to.
	laddr := l.LocalAddr().(*net.UDPAddr)
	log.Printf("Listening on: %s", laddr)
	b := [1024]byte{}
	l.SetReadBuffer(len(b))

	// Read loop.
	var out []Found
	done := make(chan struct{})
	go func() {
		defer func() {
			done <- struct{}{}
		}()
		for {
			n, src, err := l.ReadFromUDP(b[:])
			log.Printf("ReadFromUDP() = %v, %v, %v", n, src, err)
			if err != nil {
				// Ignore read errors since it'll fail when the connection is closed.
				break
			}
			// TODO(maruel): It's a 140 bytes packet. Figure out the format.
			if i := bytes.IndexByte(b[:n], 0); i != -1 {
				out = append(out, Found{IP: src.IP, Name: string(b[:i])})
			}
			if first {
				return
			}
		}
	}()

	// It seems that the content is ignored in practice, and that the printer
	// replies to the UDP packet origin IP:port anyway.
	magic := [8]byte{}
	copy(magic[:4], laddr.IP)
	binary.BigEndian.PutUint16(magic[4:], uint16(laddr.Port))
	log.Printf("Magic: %x", magic)
	if _, err := l.WriteTo(magic[:], raddr); err != nil {
		l.Close()
		<-done
		return nil, fmt.Errorf("failed to write magic packet: %w", err)
	}

	select {
	case <-time.After(d):
		err = l.Close()
		<-done
	case <-done:
		err = l.Close()
	}
	return out, err
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

// Query

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
			re := regexp.MustCompile(`^X: (\d+) Y: (\d+) Z: (\d+)$`)
			m := re.FindStringSubmatch(line)
			if m == nil {
				return fmt.Errorf("unknown reply: %q", line)
			}
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
		case strings.HasPrefix(line, "Tool Count: "):
			if i.ExtruderCount, err = strconv.Atoi(line[len("Tool Count: "):]); err != nil {
				return err
			}
		case strings.HasPrefix(line, "Mac Address: "):
			i.MacAddr = line[len("Mac Address: "):]
		case line == "":
		default:
			return fmt.Errorf("unknown reply: %q", line)
		}
	}
	return nil
}

// QueryStatus returns the current printer status.
func (d *Dev) QueryStatus(s *Status) error {
	resp, err := d.sendCommand("M119")
	if err != nil {
		return err
	}
	// TODO(maruel): Implement.
	for _, line := range strings.Split(resp, "\n") {
		line = strings.TrimRight(line, "\r")
		switch {
		case line == "Endstop: X-max:0 Y-max:0 Z-max:0":
		case strings.HasPrefix(line, "MachineStatus: "):
			// PAUSED
		case strings.HasPrefix(line, "MoveMode: "):
			// PAUSED
		case line == "Status: S:0 L:0 J:0 F:0":
		default:
			return fmt.Errorf("unknown reply: %q", line)
		}
	}
	return nil
}

// QueryExtruderPosition returns the current extruder position.
func (d *Dev) QueryExtruderPosition(p *Position) error {
	resp, err := d.sendCommand("M114")
	if err != nil {
		return err
	}
	re := regexp.MustCompile(`^X:(\-?\d+(?:\.\d+)) Y:(\-?\d+(?:\.\d+)) Z:(\-?\d+(?:\.\d+)) A:(\d+) B:(\d+)$`)
	m := re.FindStringSubmatch(resp)
	if m == nil {
		return fmt.Errorf("unknown reply: %q", resp)
	}
	v, err := parseDistance(m[1])
	if err != nil {
		return err
	}
	p.X = v

	if v, err = parseDistance(m[2]); err != nil {
		return err
	}
	p.Y = v

	if v, err = parseDistance(m[3]); err != nil {
		return err
	}
	p.Z = v

	w, err := strconv.Atoi(m[4])
	if err != nil {
		return err
	}
	p.A = w
	if w, err = strconv.Atoi(m[5]); err != nil {
		return err
	}
	p.B = w
	return nil
}

// QueryTemp queries the temperatures.
func (d *Dev) QueryTemp(t *Temperatures) error {
	resp, err := d.sendCommand("M105")
	if err != nil {
		return err
	}
	// TODO(maruel): Parse.
	re := regexp.MustCompile(`^T(\d+):(\d+) /(\d+) B:(\d+)/(\d+)$`)
	m := re.FindStringSubmatch(resp)
	if m == nil {
		return fmt.Errorf("unknown reply: %q", resp)
	}
	return nil
}

// QueryJobStatus returns the current job status.
func (d *Dev) QueryJobStatus() (string, error) {
	// M27 S2 reports every 2 seconds.
	resp, err := d.sendCommand("M27")
	if err != nil {
		return "", err
	}
	// "SD printing byte 0/100"
	return resp, nil
}

// Commands

// SetLight turns the printer's light on or off.
func (d *Dev) SetLight(on bool) error {
	// Channels must be lowercase. Duh.
	cmd := "M146 r0 g0 b0 F0"
	if on {
		cmd = "M146 r255 g255 b255 F0"
	}
	resp, err := d.sendCommand(cmd)
	if resp != "" {
		return fmt.Errorf("unknown reply: %q", resp)
	}
	return err
}

// SetFan turns the printer's fan on or off.
func (d *Dev) SetFan(on bool) error {
	// TODO(maruel): It turns back on right after!
	// TODO(maruel): Doesn't work.
	cmd := "M107 P0"
	if on {
		cmd = "M106 P0 S255"
	}
	resp, err := d.sendCommand(cmd)
	if resp != "" {
		return fmt.Errorf("unknown reply: %q", resp)
	}
	return err
}

// StopJob stops the running job.
func (d *Dev) StopJob() error {
	resp, err := d.sendCommand("M26")
	if resp != "" {
		return fmt.Errorf("unknown reply: %q", resp)
	}
	return err
}

// SendRawCommand sends a raw command, returns the trimmed response.
func (d *Dev) SendRawCommand(cmd string) (string, error) {
	return d.sendCommand(cmd)
}

// Internal

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
	//log.Printf("sendCommand(%q)", cmd)
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
	if strings.HasSuffix(line, "\r\n") {
		line = line[:len(line)-len("\r\n")]
	}
	log.Printf("sendCommand(%q): %q", cmd, line)
	return line, nil
}

func parseDistance(s string) (physic.Distance, error) {
	// It seems the printer handlers this as a float but handle as integer here.
	neg := s[0] == '-'
	if neg {
		s = s[1:]
	}
	var out physic.Distance
	if i := strings.IndexRune(s, '.'); i != -1 {
		v, err := strconv.Atoi(s[:i])
		if err != nil {
			return 0, err
		}
		f, err := strconv.Atoi(s[i+1:])
		if err != nil {
			return 0, err
		}
		ff := physic.MilliMetre
		for j := 0; j < len(s[i+1:]); j++ {
			ff /= 10
		}
		out = physic.MilliMetre*physic.Distance(v) + ff*physic.Distance(f)
	} else {
		v, err := strconv.Atoi(s)
		if err != nil {
			return 0, err
		}
		out = physic.MilliMetre * physic.Distance(v)
	}
	if neg {
		out *= -1
	}
	return out, nil
}
