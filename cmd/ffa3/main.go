// Copyright 2021 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

package main

import (
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/maruel/ffa3"
)

func play(d *ffa3.Dev) error {
	i := ffa3.Info{}
	if err := d.QueryPrinterInfo(&i); err != nil {
		return err
	}
	fmt.Printf("Printer info: %# v\n", i)

	p := ffa3.Position{}
	if err := d.QueryExtruderPosition(&p); err != nil {
		return err
	}
	fmt.Printf("Extruder position: %# v\n", p)

	s := ffa3.Status{}
	if err := d.QueryStatus(&s); err != nil {
		return err
	}
	fmt.Printf("Status: %# v\n", s)

	t := ffa3.Temperatures{}
	if err := d.QueryTemp(&t); err != nil {
		return err
	}
	fmt.Printf("Temperatures: %# v\n", t)

	j, err := d.QueryJobStatus()
	if err != nil {
		return err
	}
	fmt.Printf("Job: %s\n", j)

	if err := d.StopJob(); err != nil {
		return err
	}

	resp, err := d.SendRawCommand("G28")
	fmt.Printf("Response: %q\n", resp)
	fmt.Printf("Err: %v\n", err)

	/*
		fmt.Printf("LED off\n")
		if err := d.SetLight(false); err != nil {
			return err
		}
		time.Sleep(time.Second)

		fmt.Printf("LED on\n")
		if err := d.SetLight(true); err != nil {
			return err
		}
		time.Sleep(time.Second)
	*/

	/*
		fmt.Printf("Fan off\n")
		if err := d.SetFan(false); err != nil {
			return err
		}
		time.Sleep(time.Second)

		fmt.Printf("Fan on\n")
		if err := d.SetFan(true); err != nil {
			return err
		}
		time.Sleep(4 * time.Second)

		fmt.Printf("Fan off\n")
		if err := d.SetFan(false); err != nil {
			return err
		}
		time.Sleep(time.Second)
	*/
	return nil
}

func mainImpl() error {
	ip := flag.String("ip", "", "Printer IP; by default a search is done but it takes one second")
	verbose := flag.Bool("v", false, "verbose")
	flag.Parse()
	if !*verbose {
		log.SetOutput(ioutil.Discard)
	} else {
		log.SetFlags(log.Lmicroseconds)
	}

	if *ip == "" {
		f, err := ffa3.Search(true, time.Second)
		if err != nil {
			return err
		}
		if len(f) == 0 {
			return errors.New("no printer found on network")
		}
		// This will not happen since we specify first=true above. Leaving
		// the code as a safety gap in case I want to change first flag above.
		if len(f) > 1 {
			var s []string
			for _, l := range f {
				s = append(s, "- "+l.String())
			}
			sort.Strings(s)
			return fmt.Errorf("more than one printer found on network; specify which one you want:\n%s", strings.Join(s, "\n"))
		}
		log.Printf("Using printer: %s", f[0].String())
		*ip = f[0].IP.String()
	}

	d, err := ffa3.Connect(*ip)
	if err != nil {
		return err
	}
	err = play(d)
	if err2 := d.Close(); err == nil {
		err = err2
	}
	return err
}

func main() {
	if err := mainImpl(); err != nil {
		fmt.Fprintf(os.Stderr, "ffa3: %s\n", err)
		os.Exit(1)
	}
}
