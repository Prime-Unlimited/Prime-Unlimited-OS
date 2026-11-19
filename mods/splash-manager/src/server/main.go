package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const (
	listenAddr = "0.0.0.0:8081"
	maxUpload  = 10 << 20 // 10 MiB

	// Engine OS splash paths under the data partition.
	// Engine reads from locale packs; we write both 16:10 and 16:9 variants
	// into every locale so the splash appears regardless of language setting.
	engineSplashBase = "/data/enginex/ui/packs"
)

var locales = []string{"en", "zh-Hans", "zh-Hant"}

// splashTargets returns all paths that need to be written for a given
// aspect ratio filename.
func splashTargets(filename string) []string {
	targets := make([]string, 0, len(locales))
	for _, loc := range locales {
		targets = append(targets, filepath.Join(engineSplashBase, loc, "Skins", "Embedded", "img", filename))
	}
	return targets
}

func main() {
	http.HandleFunc("/", handleIndex)
	http.HandleFunc("/upload", handleUpload)
	http.HandleFunc("/current.png", handleCurrent)

	log.Printf("Splash server listening on %s", listenAddr)
	if err := http.ListenAndServe(listenAddr, nil); err != nil {
		log.Fatal(err)
	}
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, indexHTML)
}

func handleCurrent(w http.ResponseWriter, r *http.Request) {
	// Serve the first existing splash as a preview.
	targets := splashTargets("SplashScreen.png")
	for _, path := range targets {
		if data, err := os.ReadFile(path); err == nil {
			w.Header().Set("Content-Type", "image/png")
			w.Write(data)
			return
		}
	}
	http.NotFound(w, r)
}

func handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body := http.MaxBytesReader(w, r.Body, maxUpload)
	data, err := io.ReadAll(body)
	if err != nil {
		http.Error(w, "upload too large (max 10 MiB)", http.StatusRequestEntityTooLarge)
		return
	}

	if !validPNG(data) {
		http.Error(w, "not a valid PNG file", http.StatusBadRequest)
		return
	}

	width, height := pngDimensions(data)

	// Determine which splash files to write based on aspect ratio.
	var filenames []string
	ratio := float64(width) / float64(height)
	if ratio > 1.7 { // ~16:9
		filenames = append(filenames, "SplashScreen_16x9.png")
	}
	if ratio < 1.7 { // ~16:10
		filenames = append(filenames, "SplashScreen.png")
	}
	if len(filenames) == 0 {
		// Ambiguous ratio — write both variants.
		filenames = []string{"SplashScreen.png", "SplashScreen_16x9.png"}
	}

	var errors []string
	written := 0
	for _, fn := range filenames {
		for _, target := range splashTargets(fn) {
			dir := filepath.Dir(target)
			if err := os.MkdirAll(dir, 0755); err != nil {
				errors = append(errors, fmt.Sprintf("%s: mkdir: %v", target, err))
				continue
			}
			if err := os.WriteFile(target, data, 0644); err != nil {
				errors = append(errors, fmt.Sprintf("%s: write: %v", target, err))
				continue
			}
			written++
		}
	}

	if written == 0 {
		http.Error(w, "could not write splash files: "+strings.Join(errors, "; "), http.StatusInternalServerError)
		return
	}

	log.Printf("splash updated: %dx%d, %d bytes, %d files written", width, height, len(data), written)
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "OK: %d files updated", written)
}

// validPNG checks the PNG magic bytes.
func validPNG(data []byte) bool {
	return len(data) >= 8 && bytes.Equal(data[:8], []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a})
}

// pngDimensions extracts width and height from the IHDR chunk.
func pngDimensions(data []byte) (uint32, uint32) {
	if len(data) < 24 {
		return 0, 0
	}
	w := binary.BigEndian.Uint32(data[16:20])
	h := binary.BigEndian.Uint32(data[20:24])
	return w, h
}

const indexHTML = `<!DOCTYPE html><html><head><meta charset='utf-8'><meta name='viewport' content='width=device-width,initial-scale=1'><title>Splash Manager</title><style>*{margin:0;padding:0;box-sizing:border-box}body{background:#1a1a1a;color:#e0e0e0;font-family:-apple-system,sans-serif;min-height:100vh;display:flex;align-items:center;justify-content:center;overflow:hidden}body::before,body::after{content:'';position:fixed;right:-80px;top:50%;transform:translateY(-50%);border-radius:50%;border:2px solid #333;opacity:.3}body::before{width:460px;height:460px}body::after{width:320px;height:320px;border-color:#444}.c{max-width:600px;width:90%;z-index:1}.t{font-size:.7em;color:#666;letter-spacing:2px;text-transform:uppercase;margin-bottom:1em}.h{font-size:2.2em;font-weight:900;color:#fff;line-height:1.1;margin-bottom:.15em}.h span{color:#ff6b1a}.sub{color:#888;margin-bottom:1.8em}.drop{border:2px dashed #444;border-radius:12px;padding:48px 20px;cursor:pointer;transition:.3s;text-align:center}.drop:hover,.drop.ov{border-color:#ff6b1a;background:rgba(255,107,26,.05)}.drop p{color:#aaa}.drop b{font-size:2.5em;display:block;margin-bottom:.4em}.pv{margin-top:1.5em;text-align:center}.pv img{max-width:100%;border-radius:8px;box-shadow:0 4px 20px #0008}.st{margin-top:1em;padding:10px;border-radius:8px;display:none;text-align:center}.ok{display:block;background:#1a2e1a;color:#4caf50}.er{display:block;background:#2e1a1a;color:#f44}.up{display:block;background:#2e2010;color:#ff6b1a}.cu{margin-top:1.5em;opacity:.5;text-align:center}.cu img{max-width:100%;border-radius:8px}button{background:#ff6b1a;color:#fff;border:none;padding:12px 28px;border-radius:8px;font-size:1em;cursor:pointer;margin-top:1em;font-weight:700}button:hover{background:#e55b10}button:disabled{opacity:.4}</style></head><body><div class='c'><div class='t'>Prime Unlimited OS</div><h1 class='h'>Splash <span>Manager</span></h1><p class='sub'>Upload a custom boot splash for your Denon DJ</p><div class='drop' id='d'><b>&#x1F3A8;</b><p>Drop a PNG here<br><small style='color:#666'>1280x800 (16:10) or 1280x720 (16:9)</small></p><input type='file' id='f' accept='image/png' style='display:none'></div><div class='pv' id='pv' style='display:none'><img id='pi'><br><button id='b' onclick='U()'>Apply as Boot Splash</button></div><div class='st' id='st'></div><div class='cu'><img src='/current.png' onerror='this.parentElement.style.display="none"'></div></div><script>var D=null,dr=document.getElementById('d'),fi=document.getElementById('f');dr.onclick=function(){fi.click()};dr.ondragover=function(e){e.preventDefault();dr.classList.add('ov')};dr.ondragleave=function(){dr.classList.remove('ov')};dr.ondrop=function(e){e.preventDefault();dr.classList.remove('ov');H(e.dataTransfer.files[0])};fi.onchange=function(){H(fi.files[0])};function H(f){if(!f||!/\.png$/i.test(f.name)){S('er','Select a PNG file');return}var r=new FileReader;r.onload=function(e){D=new Uint8Array(e.target.result);document.getElementById('pi').src=URL.createObjectURL(f);document.getElementById('pv').style.display='block';S('','')};r.readAsArrayBuffer(f)}function U(){if(!D)return;var b=document.getElementById('b');b.disabled=true;S('up','Uploading...');var x=new XMLHttpRequest;x.open('POST','/upload');x.setRequestHeader('Content-Type','image/png');x.onload=function(){x.status==200?S('ok','Splash updated! Reboot to see it.'):S('er','Failed: '+x.responseText);b.disabled=false};x.onerror=function(){S('er','Connection error');b.disabled=false};x.send(D)}function S(c,m){var s=document.getElementById('st');s.className='st';if(c){s.classList.add(c);s.textContent=m}}</script></body></html>`
