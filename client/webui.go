package main

import (
	"os"
	"fmt"
	"time"
	"sort"
	"sync"
	"bytes"
	"strings"
	"runtime"
	"net/http"
	"io/ioutil"
	"archive/zip"
	"path/filepath"
	"github.com/piotrnar/gocoin/btc"
)

var webuimenu = [][2]string {
	{"/", "Home"},
	{"/net", "Network"},
	{"/txs", "Transactions"},
	{"/blocks", "Blocks"},
	{"/miners", "Miners"},
	{"/counts", "Counters"},
}

const htmlhead = `<script type="text/javascript" src="webui/gocoin.js"></script>
<link rel="stylesheet" href="webui/gocoin.css" type="text/css"></head><body>
<table align="center" width="990" cellpadding="0" cellspacing="0"><tr><td>
`

func load_template(fn string) string {
	dat, _ := ioutil.ReadFile("webht/"+fn)
	return string(dat)
}


func p_webui(w http.ResponseWriter, r *http.Request) {
	if len(strings.SplitN(r.URL.Path[1:], "/", 3))==2 {
		dat, _ := ioutil.ReadFile(r.URL.Path[1:])
		if len(dat)>0 {
			switch filepath.Ext(r.URL.Path) {
				case ".js": w.Header()["Content-Type"] = []string{"text/javascript"}
				case ".css": w.Header()["Content-Type"] = []string{"text/css"}
			}
			w.Write(dat)
		}
	}
}

func write_html_head(w http.ResponseWriter, r *http.Request) {
	s := load_template("page_head.html")
	s = strings.Replace(s, "{VERSION}", btc.SourcesTag, 1)
	if CFG.Testnet {
		s = strings.Replace(s, "{TESTNET}", "Testnet ", 1)
	} else {
		s = strings.Replace(s, "{TESTNET}", "", 1)
	}
	for i := range webuimenu {
		var x string
		if i>0 && i<len(webuimenu)-1 {
			x = " | "
		}
		x += "<a "
		if r.URL.Path==webuimenu[i][0] {
			x += "class=\"menuat\" "
		}
		x += "href=\""+webuimenu[i][0]+"\">"+webuimenu[i][1]+"</a>"
		if i==len(webuimenu)-1 {
			s = strings.Replace(s, "{MENU_LEFT}", "", 1)
			s = strings.Replace(s, "{MENU_RIGHT}", x, 1)
		} else {
			s = strings.Replace(s, "{MENU_LEFT}", x+"{MENU_LEFT}", 1)
		}
	}
	w.Write([]byte(s))
}

func write_html_tail(w http.ResponseWriter) {
	dat, _ := ioutil.ReadFile("webht/page_tail.html")
	w.Write(dat)
}

func p_home(w http.ResponseWriter, r *http.Request) {
	s := load_template("home.html")

	mutex.Lock()
	s = strings.Replace(s, "{TOTAL_BTC}", fmt.Sprintf("%.8f", float64(LastBalance)/1e8), 1)
	s = strings.Replace(s, "{UNSPENT_OUTS}", fmt.Sprint(len(MyBalance)), 1)
	s = strings.Replace(s, "{LAST_BLOCK_HASH}", LastBlock.BlockHash.String(), 1)
	s = strings.Replace(s, "{LAST_BLOCK_HEIGHT}", fmt.Sprint(LastBlock.Height), 1)
	s = strings.Replace(s, "{LAST_BLOCK_TIME}",
		time.Unix(int64(LastBlock.Timestamp), 0).Format("2006/01/02 15:04:05"), 1)
	s = strings.Replace(s, "{LAST_BLOCK_DIFF}", fmt.Sprintf("%.3f", btc.GetDifficulty(LastBlock.Bits)), 1)
	s = strings.Replace(s, "{LAST_BLOCK_RCVD}", time.Now().Sub(LastBlockReceived).String(), 1)
	s = strings.Replace(s, "{BLOCKS_CACHED}", fmt.Sprint(len(cachedBlocks)), 1)
	s = strings.Replace(s, "{KNOWN_PEERS}", fmt.Sprint(peerDB.Count()), 1)
	s = strings.Replace(s, "{NODE_UPTIME}", time.Now().Sub(StartTime).String(), 1)
	s = strings.Replace(s, "{NET_BLOCK_QSIZE}", fmt.Sprint(len(netBlocks)), 1)
	s = strings.Replace(s, "{NET_TX_QSIZE}", fmt.Sprint(len(netTxs)), 1)
	s = strings.Replace(s, "{OPEN_CONNS_TOTAL}", fmt.Sprint(len(openCons)), 1)
	s = strings.Replace(s, "{OPEN_CONNS_OUT}", fmt.Sprint(OutConsActive), 1)
	s = strings.Replace(s, "{OPEN_CONNS_IN}", fmt.Sprint(InConsActive), 1)
	mutex.Unlock()

	bw_mutex.Lock()
	tick_recv()
	tick_sent()
	s = strings.Replace(s, "{DL_SPEED_NOW}", fmt.Sprint(dl_bytes_prv_sec>>10), 1)
	s = strings.Replace(s, "{DL_SPEED_MAX}", fmt.Sprint(DownloadLimit>>10), 1)
	s = strings.Replace(s, "{DL_TOTAL}", bts(dl_bytes_total), 1)
	s = strings.Replace(s, "{UL_SPEED_NOW}", fmt.Sprint(ul_bytes_prv_sec>>10), 1)
	s = strings.Replace(s, "{UL_SPEED_MAX}", fmt.Sprint(UploadLimit>>10), 1)
	s = strings.Replace(s, "{UL_TOTAL}", bts(ul_bytes_total), 1)
	bw_mutex.Unlock()


	ExternalIpMutex.Lock()
	for ip, cnt := range ExternalIp4 {
		s = strings.Replace(s, "{ONE_EXTERNAL_IP}",
			fmt.Sprintf("%dx%d.%d.%d.%d&nbsp;&nbsp;{ONE_EXTERNAL_IP}", cnt,
				byte(ip>>24), byte(ip>>16), byte(ip>>8), byte(ip)), 1)
	}
	ExternalIpMutex.Unlock()
	s = strings.Replace(s, "{ONE_EXTERNAL_IP}", "", 1)

	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	s = strings.Replace(s, "{HEAP_SIZE_MB}", fmt.Sprint(ms.Alloc>>20), 1)
	s = strings.Replace(s, "{SYSMEM_USED_MB}", fmt.Sprint(ms.Sys>>20), 1)
	s = strings.Replace(s, "{ECDSA_VERIFY_COUNT}", fmt.Sprint(btc.EcdsaVerifyCnt), 1)

	write_html_head(w, r)
	w.Write([]byte(s))
	write_html_tail(w)
}

func p_net(w http.ResponseWriter, r *http.Request) {
	net_page := load_template("net.html")
	net_row := load_template("net_row.html")

	mutex.Lock()
	srt := make(sortedkeys, len(openCons))
	cnt := 0
	for k, v := range openCons {
		srt[cnt].key = k
		srt[cnt].ConnID = v.ConnID
		cnt++
	}
	mutex.Unlock()
	sort.Sort(srt)
	net_page = strings.Replace(net_page, "{OUT_CONNECTIONS}", fmt.Sprint(OutConsActive), 1)
	net_page = strings.Replace(net_page, "{IN_CONNECTIONS}", fmt.Sprint(InConsActive), 1)

	for idx := range srt {
		v := openCons[srt[idx].key]
		s := net_row

		s = strings.Replace(s, "{CONNID}", fmt.Sprint(v.ConnID), 2)
		if v.Incomming {
			s = strings.Replace(s, "{CONN_DIR_ICON}", "<img src=\"webui/incoming.png\">", 1)
		} else {
			s = strings.Replace(s, "{CONN_DIR_ICON}", "<img src=\"webui/outgoing.png\">", 1)
		}

		s = strings.Replace(s, "{PEER_ADDR}", v.PeerAddr.Ip(), 1)
		s = strings.Replace(s, "{PERR_PING}", fmt.Sprint(v.GetAveragePing()), 1)
		s = strings.Replace(s, "{LAST_RCVD_LEN}", fmt.Sprint(v.LastBtsRcvd), 1)
		s = strings.Replace(s, "{LAST_RCVD_CMD}", v.LastCmdRcvd, 1)
		s = strings.Replace(s, "{LAST_SENT_LEN}", fmt.Sprint(v.LastBtsSent), 1)
		s = strings.Replace(s, "{LAST_SENT_CNT}", v.LastCmdSent, 1)
		s = strings.Replace(s, "{TOTAL_RCVD}", bts(v.BytesReceived), 1)
		s = strings.Replace(s, "{TOTAL_SENT}", bts(v.BytesSent), 1)
		s = strings.Replace(s, "{NODE_VERSION}", fmt.Sprint(v.node.version), 1)
		s = strings.Replace(s, "{USER_AGENT}", v.node.agent, 1)
		s = strings.Replace(s, "{SENDING_DONE}", fmt.Sprint(v.send.sofar), 1)
		s = strings.Replace(s, "{SENDING_TOTAL}", fmt.Sprint(len(v.send.buf)), 1)
		s = strings.Replace(s, "{BLOCKS_IN_PROGRESS}", fmt.Sprint(len(v.GetBlockInProgress)), 1)

		net_page = strings.Replace(net_page, "{PEER_ROW}", s+"\n{PEER_ROW}", 1)
	}
	net_page = strings.Replace(net_page, "{PEER_ROW}", "", 1)

	write_html_head(w, r)
	w.Write([]byte(net_page))
	write_html_tail(w)
}

func p_txs(w http.ResponseWriter, r *http.Request) {
	var txloadresult string
	var wg sync.WaitGroup

	// Check if there is a tx upload request
	r.ParseMultipartForm(2e6)
	fil, _, _ := r.FormFile("txfile")
	if fil != nil {
		tx2in, _ := ioutil.ReadAll(fil)
		if len(tx2in)>0 {
			wg.Add(1)
			req := &oneUiReq{param:string(tx2in)}
			req.done.Add(1)
			req.handler = func(dat string) {
				txloadresult = load_raw_tx([]byte(dat))
				wg.Done()
			}
			uiChannel <- req
		}
	}


	s := load_template("txs.html")
	tx_mutex.Lock()
	s = strings.Replace(s, "{T2S_CNT}", fmt.Sprint(len(TransactionsToSend)), 1)
	s = strings.Replace(s, "{TRE_CNT}", fmt.Sprint(len(TransactionsRejected)), 1)
	s = strings.Replace(s, "{PTR1_CNT}", fmt.Sprint(len(TransactionsPending)), 1)
	s = strings.Replace(s, "{PTR2_CNT}", fmt.Sprint(len(netTxs)), 1)
	s = strings.Replace(s, "{SPENT_OUTS_CNT}", fmt.Sprint(len(SpentOutputs)), 1)
	s = strings.Replace(s, "{AWAITING_INPUTS}", fmt.Sprint(len(WaitingForInputs)), 1)
	tx_mutex.Unlock()

	var ld string
	wg.Wait()
	if txloadresult!="" {
		ld = load_template("txs_load.html")
		ld = strings.Replace(ld, "{TX_RAW_DATA}", txloadresult, 1)
	}
	s = strings.Replace(s, "{TX_LOAD}", ld, 1)

	write_html_head(w, r)
	w.Write([]byte(s))
	write_html_tail(w)
}

func p_blocks(w http.ResponseWriter, r *http.Request) {
	blks := load_template("blocks.html")
	onerow := load_template("blocks_row.html")

	end := BlockChain.BlockTreeEnd
	for cnt:=0; end!=nil && cnt<100; cnt++ {
		bl, _, e := BlockChain.Blocks.BlockGet(end.BlockHash)
		if e != nil {
			return
		}
		block, e := btc.NewBlock(bl)
		if e!=nil {
			return
		}
		block.BuildTxList()
		s := onerow

		s = strings.Replace(s, "{BLOCK_NUMBER}", fmt.Sprint(end.Height), 1)
		s = strings.Replace(s, "{BLOCK_TIMESTAMP}",
			time.Unix(int64(block.BlockTime), 0).Format("2006-01-02 15:04:05"), 1)
		s = strings.Replace(s, "{BLOCK_HASH}", end.BlockHash.String(), 1)
		s = strings.Replace(s, "{BLOCK_TXS}", fmt.Sprint(len(block.Txs)), 1)
		s = strings.Replace(s, "{BLOCK_SIZE}", fmt.Sprint(len(bl)), 1)
		var rew uint64
		for o := range block.Txs[0].TxOut {
			rew += block.Txs[0].TxOut[o].Value
		}
		s = strings.Replace(s, "{BLOCK_REWARD}", fmt.Sprintf("%.2f", float64(rew)/1e8), 1)
		s = strings.Replace(s, "{BLOCK_MINER}", blocks_miner(bl), 1)

		blks = strings.Replace(blks, "{BLOCK_ROW}", s+"{BLOCK_ROW}", 1)

		end = end.Parent
	}
	blks = strings.Replace(blks, "{BLOCK_ROW}", "", 1)

	write_html_head(w, r)
	w.Write([]byte(blks))
	write_html_tail(w)
}

type onemiernstat []struct {
	name string
	cnt int
}

func (x onemiernstat) Len() int {
	return len(x)
}

func (x onemiernstat) Less(i, j int) bool {
	if x[i].cnt == x[j].cnt {
		return x[i].name < x[j].name // Same numbers: sort by name
	}
	return x[i].cnt > x[j].cnt
}

func (x onemiernstat) Swap(i, j int) {
	x[i], x[j] = x[j], x[i]
}

func p_miners(w http.ResponseWriter, r *http.Request) {
	m := make(map[string]int, 20)
	cnt, unkn := 0, 0
	end := BlockChain.BlockTreeEnd
	var lastts int64
	var diff float64
	now := time.Now().Unix()
	for ; end!=nil; cnt++ {
		if now-int64(end.Timestamp) > 24*3600 {
			break
		}
		lastts = int64(end.Timestamp)
		bl, _, e := BlockChain.Blocks.BlockGet(end.BlockHash)
		if e != nil {
			return
		}
		diff += btc.GetDifficulty(end.Bits)
		miner := blocks_miner(bl)
		if miner!="" {
			m[miner]++
		} else {
			unkn++
		}
		end = end.Parent
	}
	srt := make(onemiernstat, len(m))
	i := 0
	for k, v := range m {
		srt[i].name = k
		srt[i].cnt = v
		i++
	}
	sort.Sort(srt)

	mnrs := load_template("miners.html")
	onerow := load_template("miners_row.html")

	diff /= float64(cnt)
	bph := float64(cnt)/24
	hrate := bph/6 * diff * 7158278.826667
	mnrs = strings.Replace(mnrs, "{BLOCKS_COUNT}", fmt.Sprint(cnt), 1)
	mnrs = strings.Replace(mnrs, "{FIRST_BLOCK_TIME}", time.Unix(lastts, 0).Format("2006-01-02 15:04:05"), 1)
	mnrs = strings.Replace(mnrs, "{AVG_BLOCKS_PER_HOUR}", fmt.Sprintf("%.2f", bph), 1)
	mnrs = strings.Replace(mnrs, "{AVG_DIFFICULTY}", fmt.Sprintf("%.2f", diff), 1)
	mnrs = strings.Replace(mnrs, "{AVG_HASHDATE}", hr2str(hrate), 1)


	for i := range srt {
		s := onerow
		s = strings.Replace(s, "{MINER_NAME}", srt[i].name, 1)
		s = strings.Replace(s, "{BLOCK_COUNT}", fmt.Sprint(srt[i].cnt), 1)
		s = strings.Replace(s, "{TOTAL_PERCENT}", fmt.Sprintf("%.0f", 100*float64(srt[i].cnt)/float64(cnt)), 1)
		s = strings.Replace(s, "{MINER_HASHRATE}", hr2str(hrate*float64(srt[i].cnt)/float64(cnt)), 1)
		mnrs = strings.Replace(mnrs, "{MINER_ROW}", s+"{MINER_ROW}", 1)
	}

	onerow = strings.Replace(onerow, "{MINER_NAME}", "<i>Unknown</i>", 1)
	onerow = strings.Replace(onerow, "{BLOCK_COUNT}", fmt.Sprint(unkn), 1)
	onerow = strings.Replace(onerow, "{TOTAL_PERCENT}", fmt.Sprintf("%.0f", 100*float64(unkn)/float64(cnt)), 1)
	onerow = strings.Replace(onerow, "{MINER_HASHRATE}", hr2str(hrate*float64(unkn)/float64(cnt)), 1)
	mnrs = strings.Replace(mnrs, "{MINER_ROW}", onerow, 1)

	write_html_head(w, r)
	w.Write([]byte(mnrs))
	write_html_tail(w)
}

func p_counts(w http.ResponseWriter, r *http.Request) {
	write_html_head(w, r)
	counter_mutex.Lock()
	ck := make([]string, 0)
	for k, _ := range Counter {
		ck = append(ck, k)
	}
	sort.Strings(ck)
	fmt.Fprint(w, "<table class=\"mono\"><tr>")
	fmt.Fprint(w, "<td valign=\"top\"><table class=\"bord\"><tr><th colspan=\"2\">Generic Counters")
	prv_ := ""
	for i := range ck {
		if ck[i][4]=='_' {
			if ck[i][:4]!=prv_ {
				prv_ = ck[i][:4]
				fmt.Fprint(w, "</table><td valign=\"top\"><table class=\"bord\"><tr><th colspan=\"2\">")
				switch prv_ {
					case "rbts": fmt.Fprintln(w, "Received bytes")
					case "rcvd": fmt.Fprintln(w, "Received messages")
					case "sbts": fmt.Fprintln(w, "Sent bytes")
					case "sent": fmt.Fprintln(w, "Sent messages")
					default: fmt.Fprintln(w, prv_)
				}
			}
			fmt.Fprintf(w, "<tr><td>%s</td><td>%d</td></tr>\n", ck[i][5:], Counter[ck[i]])
		} else {
			fmt.Fprintf(w, "<tr><td>%s</td><td>%d</td></tr>\n", ck[i], Counter[ck[i]])
		}
	}
	fmt.Fprint(w, "</table></table>")
	counter_mutex.Unlock()
	write_html_tail(w)
}

func raw_balance(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte(UpdateBalanceFolder()))
}

func xml_balance(w http.ResponseWriter, r *http.Request) {
	w.Header()["Content-Type"] = []string{"text/xml"}
	w.Write([]byte("<unspent>"))

	//For safety, lets get the balance from teh main thread
	var wg sync.WaitGroup
	wg.Add(1)
	req := new(oneUiReq)
	req.done.Add(1)
	req.handler = func(dat string) {
		for i := range MyBalance {
			w.Write([]byte("<output>"))
			fmt.Fprint(w, "<txid>", btc.NewUint256(MyBalance[i].TxPrevOut.Hash[:]).String(), "</txid>")
			fmt.Fprint(w, "<vout>", MyBalance[i].TxPrevOut.Vout, "</vout>")
			fmt.Fprint(w, "<value>", MyBalance[i].Value, "</value>")
			fmt.Fprint(w, "<inblock>", MyBalance[i].MinedAt, "</inblock>")
			fmt.Fprint(w, "<addr>", MyBalance[i].BtcAddr.String(), "</addr>")
			fmt.Fprint(w, "<label>", MyBalance[i].BtcAddr.Label, "</label>")
			w.Write([]byte("</output>"))
		}
		wg.Done()
	}
	uiChannel <- req
	wg.Wait()
	w.Write([]byte("</unspent>"))
}


func raw_net(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintln(w, "Error")
		}
	}()

	r.ParseForm()
	if len(r.Form["id"])==0 {
		fmt.Println("No id given")
		return
	}

	v := look2conn(r.Form["id"][0])
	if v == nil {
		fmt.Fprintln(w, "There is no such an active connection")
	} else {
		w.Write([]byte(node_stat(v)))
	}
}


func xmp_txs2s(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()

	if len(r.Form["del"])>0 {
		tid := btc.NewUint256FromString(r.Form["del"][0])
		if tid!=nil {
			tx_mutex.Lock()
			delete(TransactionsToSend, tid.Hash)
			tx_mutex.Unlock()
		}
	}

	if len(r.Form["send"])>0 {
		tid := btc.NewUint256FromString(r.Form["send"][0])
		if tid!=nil {
			tx_mutex.Lock()
			if ptx, ok := TransactionsToSend[tid.Hash]; ok {
				tx_mutex.Unlock()
				cnt := NetRouteInv(1, tid, nil)
				ptx.sentcnt += cnt
				ptx.lastsent = time.Now()
			}
		}
	}

	w.Header()["Content-Type"] = []string{"text/xml"}
	w.Write([]byte("<txpool>"))
	tx_mutex.Lock()
	for k, v := range TransactionsToSend {
		w.Write([]byte("<tx>"))
		fmt.Fprint(w, "<id>", btc.NewUint256(k[:]).String(), "</id>")
		fmt.Fprint(w, "<time>", v.firstseen.Unix(), "</time>")
		fmt.Fprint(w, "<len>", len(v.data), "</len>")
		fmt.Fprint(w, "<own>", v.own, "</own>")
		fmt.Fprint(w, "<sentcnt>", v.sentcnt, "</sentcnt>")
		fmt.Fprint(w, "<sentlast>", v.lastsent.Unix(), "</sentlast>")
		fmt.Fprint(w, "<volume>", v.volume, "</volume>")
		fmt.Fprint(w, "<fee>", v.fee, "</fee>")
		w.Write([]byte("</tx>"))
	}
	tx_mutex.Unlock()
	w.Write([]byte("</txpool>"))
}


func xml_txsre(w http.ResponseWriter, r *http.Request) {
	w.Header()["Content-Type"] = []string{"text/xml"}
	w.Write([]byte("<txbanned>"))
	tx_mutex.Lock()
	for _, v := range TransactionsRejected {
		w.Write([]byte("<tx>"))
		fmt.Fprint(w, "<id>", v.id.String(), "</id>")
		fmt.Fprint(w, "<time>", v.Time.Unix(), "</time>")
		fmt.Fprint(w, "<len>", v.size, "</len>")
		fmt.Fprint(w, "<reason>", v.reason, "</reason>")
		w.Write([]byte("</tx>"))
	}
	tx_mutex.Unlock()
	w.Write([]byte("</txbanned>"))
}


func dl_balance(w http.ResponseWriter, r *http.Request) {
	UpdateBalanceFolder()
	buf := new(bytes.Buffer)
	zi := zip.NewWriter(buf)
	filepath.Walk("balance/", func(path string, fi os.FileInfo, err error) error {
		if !fi.IsDir() {
			f, _ := zi.Create(path)
			if f != nil {
				da, _ := ioutil.ReadFile(path)
				f.Write(da)
			}
		}
		return nil
	})
	if zi.Close() == nil {
		w.Header()["Content-Type"] = []string{"application/zip"}
		w.Write(buf.Bytes())
	} else {
		w.Write([]byte("Error"))
	}
}


func webserver() {
	http.HandleFunc("/webui/", p_webui)
	http.HandleFunc("/net", p_net)
	http.HandleFunc("/txs", p_txs)
	http.HandleFunc("/blocks", p_blocks)
	http.HandleFunc("/miners", p_miners)
	http.HandleFunc("/counts", p_counts)

	http.HandleFunc("/txs2s.xml", xmp_txs2s)
	http.HandleFunc("/txsre.xml", xml_txsre)
	http.HandleFunc("/balance.xml", xml_balance)
	http.HandleFunc("/raw_balance", raw_balance)
	http.HandleFunc("/raw_net", raw_net)
	http.HandleFunc("/balance.zip", dl_balance)

	http.HandleFunc("/", p_home)

	http.ListenAndServe(CFG.WebUI, nil)
}
