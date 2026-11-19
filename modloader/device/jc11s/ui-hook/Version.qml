import QtQuick 2.12
import QtQuick.Layouts 1.12
import EngineOS.UI.Style 1.0
import "Base"

PrefsHeadline {
    objectName: "Version"
    readonly property string primeUnlimitedVersion: versionSource.item
        ? versionSource.item.version
        : "unknown"
    color: Style.font01Color
    font: Style.fontH2
    text: "Prime Unlimited OS " + primeUnlimitedVersion
    Layout.bottomMargin: PrefsComponentSizes.ColumnSpacings.Wider

    Loader {
        id: versionSource
        source: "file:///data/prime-unlimited/os-version.qml"
        visible: false
    }

    MouseArea {
        anchors.fill: parent
        onClicked: controlCenter.tapSecret()
    }
}
