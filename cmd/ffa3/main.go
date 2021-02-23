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
		f, err := ffa3.Search()
		if err != nil {
			return err
		}
		if len(f) == 0 {
			return errors.New("no printer found on network")
		}
		if len(f) > 1 {
			var s []string
			for _, l := range f {
				s = append(s, "- "+l.String())
			}
			sort.Strings(s)
			return fmt.Errorf("more than one printer found on network; specify which one you want:\n%s", strings.Join(s, "\n"))
		}
		log.Printf("Using printer %s", f[0])
		*ip = f[0].IP.String()
	}
	return nil
	d, err := ffa3.Connect(*ip)
	if err != nil {
		return err
	}
	i := ffa3.Info{}
	if err := d.QueryPrinterInfo(&i); err != nil {
		return err
	}
	fmt.Printf("Printer info: %# v\n", i)

	/*
		if err := d.SetLight(true); err != nil {
			return err
		}
		if err := d.SetFan(true); err != nil {
			return err
		}
		time.Sleep(time.Second)
	*/
	if err := d.SetLight(false); err != nil {
		return err
	}
	if err := d.SetFan(false); err != nil {
		return err
	}
	time.Sleep(time.Second)
	return d.Close()
}

func main() {
	if err := mainImpl(); err != nil {
		fmt.Fprintf(os.Stderr, "ffa3: %s\n", err)
		os.Exit(1)
	}
}
