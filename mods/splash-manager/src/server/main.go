package main

import (
	"bytes"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"image/png"
)

const (
	listenAddr     = "127.0.0.1:8081"
	maxUpload      = 10 << 20 // 10 MiB
	maxImagePixels = 16_000_000
	splashPath     = "/data/prime-unlimited/ui/splash/SplashScreen.png"
	tokenPath      = "/data/prime-unlimited/private/splash-manager.token"
)

var authToken string

func main() {
	var err error
	authToken, err = loadOrCreateToken(tokenPath)
	if err != nil {
		log.Fatalf("initialize upload authentication: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", handleIndex)
	mux.Handle("/auth", requireToken(http.HandlerFunc(handleAuth)))
	mux.Handle("/upload", requireToken(http.HandlerFunc(handleUpload)))
	mux.Handle("/current.png", requireToken(http.HandlerFunc(handleCurrent)))

	server := &http.Server{
		Addr:              listenAddr,
		Handler:           securityHeaders(mux),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	log.Printf("Splash Manager listening on %s", listenAddr)
	log.Printf("Password is stored in %s (read it over authenticated SSH)", tokenPath)
	log.Fatal(server.ListenAndServe())
}

func loadOrCreateToken(path string) (string, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return "", err
	}

	info, err := os.Lstat(path)
	if err == nil {
		if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			return "", fmt.Errorf("token file must be a regular file with mode 0600: %s", path)
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return "", readErr
		}
		token := strings.TrimSpace(string(data))
		if len(token) != 64 {
			return "", fmt.Errorf("token file must contain a 32-byte hex token: %s", path)
		}
		if _, decodeErr := hex.DecodeString(token); decodeErr != nil {
			return "", fmt.Errorf("token file contains invalid hex: %s", path)
		}
		return token, nil
	}
	if !os.IsNotExist(err) {
		return "", err
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := hex.EncodeToString(raw)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", err
	}
	if _, err = io.WriteString(file, token+"\n"); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		_ = os.Remove(path)
		return "", err
	}
	if closeErr != nil {
		_ = os.Remove(path)
		return "", closeErr
	}
	return token, nil
}

func requireToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const prefix = "Bearer "
		header := r.Header.Get("Authorization")
		provided := ""
		if strings.HasPrefix(header, prefix) {
			provided = strings.TrimPrefix(header, prefix)
		}
		if subtle.ConstantTimeCompare([]byte(provided), []byte(authToken)) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="Splash Manager"`)
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, indexHTML)
}

func handleCurrent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	data, err := os.ReadFile(splashPath)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	_, _ = w.Write(data)
}

func handleAuth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.Header.Get("Content-Type") != "image/png" {
		http.Error(w, "Content-Type must be image/png", http.StatusUnsupportedMediaType)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxUpload)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "upload too large (max 10 MiB)", http.StatusRequestEntityTooLarge)
		return
	}
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width < 1 || config.Height < 1 || int64(config.Width)*int64(config.Height) > maxImagePixels {
		http.Error(w, "invalid PNG or image dimensions exceed the 16 megapixel limit", http.StatusBadRequest)
		return
	}
	if _, err := png.Decode(bytes.NewReader(data)); err != nil {
		http.Error(w, "PNG data is corrupt", http.StatusBadRequest)
		return
	}

	if err := writeSplashAtomically(splashPath, data); err != nil {
		log.Printf("splash write failed: %v", err)
		http.Error(w, "could not save splash image", http.StatusInternalServerError)
		return
	}

	log.Printf("splash updated: %dx%d, %d bytes, %s", config.Width, config.Height, len(data), splashPath)
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, "Splash image saved")
}

func writeSplashAtomically(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".SplashScreen-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0644); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

const indexHTML = `<!DOCTYPE html><html><head><meta charset='utf-8'><meta name='viewport' content='width=device-width,initial-scale=1'><title>Splash Manager</title><style>*{margin:0;padding:0;box-sizing:border-box}body{background:#1a1a1a;color:#e0e0e0;font-family:-apple-system,sans-serif;min-height:100vh;display:flex;align-items:center;justify-content:center;overflow:auto;padding:24px}.c{max-width:600px;width:100%;z-index:1}.t{font-size:.7em;color:#777;letter-spacing:2px;text-transform:uppercase;margin-bottom:1em}.h{font-size:2.2em;font-weight:900;color:#fff;line-height:1.1;margin-bottom:.15em}.h span{color:#ff6b1a}.sub{color:#888;margin-bottom:1.8em}.box{border:2px solid #333;border-radius:12px;padding:24px;margin-bottom:16px}.drop{border:2px dashed #444;border-radius:12px;padding:40px 20px;cursor:pointer;transition:.3s;text-align:center}.drop:hover,.drop.ov{border-color:#ff6b1a;background:rgba(255,107,26,.05)}.drop p{color:#aaa}.drop b{font-size:2.5em;display:block;margin-bottom:.4em}input[type=password]{width:100%;margin:12px 0;background:#111;border:1px solid #444;color:#fff;padding:12px;border-radius:6px;font-size:1em}button{background:#ff6b1a;color:#fff;border:0;padding:12px 22px;border-radius:7px;font-size:1em;cursor:pointer;font-weight:700}button:hover{background:#e55b10}button:disabled{opacity:.5;cursor:wait}.pv,.cu{margin-top:16px;text-align:center}.pv img,.cu img{max-width:100%;border-radius:8px;box-shadow:0 4px 20px #0008}.st{margin-top:12px;padding:10px;border-radius:7px;display:none}.ok{display:block;background:#1a2e1a;color:#4caf50}.er{display:block;background:#2e1a1a;color:#f77}.up{display:block;background:#2e2010;color:#ff6b1a}.muted{color:#777;font-size:.85em;margin-top:8px}</style></head><body><main class='c'><div class='t'>Prime Unlimited OS</div><h1 class='h'>Splash <span>Manager</span></h1><p class='sub'>Upload a custom boot splash</p><section class='box' id='auth'><b>Unlock Splash Manager</b><p class='muted'>Enter the access token stored on the device.</p><input id='token' type='password' autocomplete='current-password' placeholder='Access token'><button id='unlock'>Unlock</button></section><section id='app' hidden><div class='drop' id='drop'><b>&#x1F3A8;</b><p>Drop a PNG here or tap to select<br><small class='muted'>PNG, up to 10 MiB</small></p><input type='file' id='file' accept='image/png' hidden></div><input type='file' id='picker' accept='image/png' hidden><div class='pv' id='preview' hidden><img id='previewImg'><br><button id='apply'>Apply as Boot Splash</button></div><div class='cu' id='current'></div></section><div class='st' id='status'></div></main><script>let key='',data=null;const $=id=>document.getElementById(id),status=(c,m)=>{let s=$('status');s.className='st';if(c){s.classList.add(c);s.textContent=m}},request=(url,opts={})=>fetch(url,{...opts,headers:{...(opts.headers||{}),Authorization:'Bearer '+key}});$('unlock').onclick=async()=>{key=$('token').value.trim();if(!key){status('er','Enter the access token');return}try{let a=await request('/auth');if(a.status===401){status('er','Access token rejected');return}if(!a.ok)throw Error('Authentication failed');let r=await request('/current.png');if(r.status===401){status('er','Access token rejected');return}if(r.ok){let img=document.createElement('img');img.src=URL.createObjectURL(await r.blob());$('current').replaceChildren(img)}$('auth').hidden=true;$('app').hidden=false;status('','')}catch(e){status('er','Could not connect to the device')}};const drop=$('drop'),picker=$('file');drop.onclick=()=>picker.click();drop.ondragover=e=>{e.preventDefault();drop.classList.add('ov')};drop.ondragleave=()=>drop.classList.remove('ov');drop.ondrop=e=>{e.preventDefault();drop.classList.remove('ov');handle(e.dataTransfer.files[0])};picker.onchange=()=>handle(picker.files[0]);function handle(f){if(!f||!f.name.match(/\.png$/i)){status('er','Select a PNG file');return}if(f.size>10*1024*1024){status('er','PNG exceeds the 10 MiB limit');return}data=f;let img=$('previewImg');img.src=URL.createObjectURL(f);$('preview').hidden=false;status('','')}$('apply').onclick=async()=>{if(!data)return;let b=$('apply');b.disabled=true;status('up','Uploading...');try{let r=await request('/upload',{method:'POST',headers:{'Content-Type':'image/png'},body:data});if(!r.ok)throw Error(await r.text());status('ok','Splash saved. Reboot the device to apply it.');}catch(e){status('er','Upload failed: '+e.message)}finally{b.disabled=false}};</script></body></html>`
