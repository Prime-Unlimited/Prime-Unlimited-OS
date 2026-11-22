#!/usr/bin/env python3
"""Build local preview artifacts; never sign, publish, install or enable a mod."""
import argparse, hashlib, json, os, stat, subprocess, tempfile, zipfile
from pathlib import Path
ROOT = Path(__file__).resolve().parents[1]

def main():
    p=argparse.ArgumentParser();p.add_argument('--target',choices=('jp24','jc11s'),required=True);p.add_argument('--output',type=Path,required=True);a=p.parse_args()
    profile=json.loads((ROOT/'src/platform'/f'{a.target}.json').read_text());version=(ROOT/'VERSION').read_text().strip()
    a.output.mkdir(parents=True,exist_ok=True);target=a.output/f'usb-copy-preview-v{version}-{a.target}.zip'
    binary_name='usb-copy-service-'+('aarch64' if a.target=='jp24' else 'armhf')
    with tempfile.TemporaryDirectory(prefix='usb-copy-package-') as temp:
        binary=Path(temp)/binary_name;env=dict(os.environ,GOOS='linux',GOARCH='arm64' if a.target=='jp24' else 'arm',GOARM='7',CGO_ENABLED='0',GOWORK='off')
        subprocess.run(['go','build','-trimpath','-buildvcs=false','-ldflags=-s -w -buildid=','-o',str(binary),'./cmd/usb-copy-service'],cwd=ROOT/'src/worker',env=env,check=True);payload=binary.read_bytes()
    service={'binary':f'usb-copy/bin/{binary_name}','sha256':hashlib.sha256(payload).hexdigest(),'capability':'media-import'}
    profile['service']=service;profile['previewOnly']=True
    files={'usb-copy/start.sh':(ROOT/'src/entrypoint.sh').read_bytes(),'usb-copy/ui/USBCopy.qml':(ROOT/'src/ui/USBCopy.qml').read_bytes(),service['binary']:payload,'usb-copy/platform.json':(json.dumps(profile,sort_keys=True)+'\n').encode()}
    for path in (ROOT/'src/worker').glob('LICENSE*'):files['usb-copy/licenses/'+path.name]=path.read_bytes()
    with zipfile.ZipFile(target,'w',zipfile.ZIP_DEFLATED,compresslevel=9) as archive:
        for name,data in sorted(files.items()):
            info=zipfile.ZipInfo(name,(2026,1,1,0,0,0));info.create_system=3;info.external_attr=(stat.S_IFREG|(0o755 if '/bin/' in name or name.endswith('.sh') else 0o600))<<16;archive.writestr(info,data,compress_type=zipfile.ZIP_DEFLATED,compresslevel=9)
    result={'id':'usb-copy','version':version,'target':a.target,'path':str(target.resolve()),'sha256':hashlib.sha256(target.read_bytes()).hexdigest(),'size':target.stat().st_size,'entrypoint':'usb-copy/start.sh','overlay':'usb-copy/ui/USBCopy.qml','service':service,'previewOnly':True,'signed':False,'deviceValidated':False}
    target.with_suffix('.metadata.json').write_text(json.dumps(result,indent=2)+'\n');print(json.dumps(result))
if __name__=='__main__':main()
