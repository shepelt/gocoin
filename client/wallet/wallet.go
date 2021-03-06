package wallet

import (
	"os"
	"fmt"
	"bufio"
	"strings"
	"path/filepath"
	"github.com/piotrnar/gocoin/btc"
	"github.com/piotrnar/gocoin/client/common"
)

const UnusedFileName = "UNUSED"

var PrecachingComplete bool

type OneWallet struct {
	FileName string
	Addrs []*btc.BtcAddr
}


func LoadWalfile(fn string, included int) (addrs []*btc.BtcAddr) {
	waldir, walname := filepath.Split(fn)
	if walname[0]=='.' {
		walname = walname[1:] // remove trailing dot (from hidden wallets)
	}
	f, e := os.Open(fn)
	if e != nil {
		println(e.Error())
		return
	}
	defer f.Close()
	rd := bufio.NewReader(f)
	linenr := 0
	for {
		var l string
		l, e = rd.ReadString('\n')
		space_first := len(l)>0 && l[0]==' '
		l = strings.Trim(l, " \t\r\n")
		linenr++
		//println(fmt.Sprint(fn, ":", linenr), "...")
		if len(l)>0 {
			if l[0]=='@' {
				if included>3 {
					println(fmt.Sprint(fn, ":", linenr), "Too many nested wallets")
				} else {
					ifn := strings.Trim(l[1:], " \n\t\t")
					addrs = append(addrs, LoadWalfile(waldir+ifn, included+1)...)
				}
			} else {
				var s string
				if l[0]!='#' {
					s = l
				} else if !PrecachingComplete && len(l)>10 && l[1]=='1' {
					s = l[1:] // While pre-caching addresses, include ones that are commented out
				}
				if s!="" {
					ls := strings.SplitN(s, " ", 2)
					if len(ls)>0 {
						a, e := btc.NewAddrFromString(ls[0])
						if e != nil {
							println(fmt.Sprint(fn, ":", linenr), e.Error())
						} else {
							a.Extra.Wallet = walname
							if len(ls)>1 {
								a.Extra.Label = ls[1]
							}
							a.Extra.Virgin = space_first
							addrs = append(addrs, a)
						}
					}
				}
			}
		}
		if e != nil {
			break
		}
	}
	return
}


// Load public wallet from a text file
func NewWallet(fn string) (wal *OneWallet) {
	wal = new(OneWallet)
	wal.FileName = fn
	wal.Addrs = LoadWalfile(fn, 0)
	return
}


func MoveToUnused(addr, walfil string) bool {
	frwal := common.GocoinHomeDir + "wallet" + string(os.PathSeparator) + walfil
	towal := common.GocoinHomeDir + "wallet" + string(os.PathSeparator) + UnusedFileName
	f, er := os.Open(frwal)
	if er != nil {
		println(er.Error())
		return false
	}
	var foundline string
	var srcwallet []string
	rd := bufio.NewReader(f)
	if rd != nil {
		for {
			ln, _, er := rd.ReadLine()
			if er !=nil {
				break
			}
			if foundline=="" && strings.HasPrefix(string(ln), addr) {
				foundline = string(ln)
			} else {
				srcwallet = append(srcwallet, string(ln))
			}
		}
	}
	f.Close()

	if foundline=="" {
		println(addr, "not found in", frwal)
		return false
	}

	f, er = os.OpenFile(towal, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0666)
	if er != nil {
		println(er.Error())
		return false
	}
	fmt.Fprintln(f, foundline)
	f.Close()

	os.Rename(frwal, frwal+".bak")

	f, er = os.Create(frwal)
	if er != nil {
		println(er.Error())
		return false
	}
	for i := range srcwallet {
		fmt.Fprintln(f, srcwallet[i])
	}
	f.Close()

	os.Remove(frwal+".bak")
	return true
}
