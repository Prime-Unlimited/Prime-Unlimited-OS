import QtQuick 2.12
import QtQuick.Layouts 1.12
import "ControlCenter/Entries"
import "ControlCenter/UberEntries"
Item{id:r;Layout.fillWidth:true;Layout.fillHeight:true
Flow{y:64;width:Math.floor(parent.width/280)*280-24;anchors.horizontalCenter:parent.horizontalCenter;spacing:24
Quantize{} CCContinue{} CCStopTime{} BeatGridEdit{} SplitCue{} CCCrossfaderEnable{} CCCrossfaderCurve{} SmartScratch{} PitchRange{} SamplerVolumeLevel{} BluetoothVolume{} TransitionFxEnable{} MixerMenuEnable{} AbletonLinkEnable{} RecordStatus{} ExternalSoftwareFx{} ComputerMode{} DayModeEnable{}
Rectangle{width:256;height:104;radius:16;color:"#080a0c";clip:true;Image{anchors.fill:parent;fillMode:Image.Stretch;source:"file:///data/prime-unlimited/ui/prime_unlimited_mod_loader.svg"}MouseArea{anchors.fill:parent;onClicked:{l.source="file:///data/prime-unlimited/ui/ModLoader.qml?"+Date.now();l.active=true}}}}
UberEntries{anchors.horizontalCenter:parent.horizontalCenter;anchors.bottom:parent.bottom;anchors.bottomMargin:32}
Loader{id:l;anchors.fill:parent;z:999999;active:false;onLoaded:if(item)item.closeRequested.connect(function(){l.active=false;l.source=""})}
}
