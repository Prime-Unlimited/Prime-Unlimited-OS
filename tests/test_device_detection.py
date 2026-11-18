from core.device_detection import DetectionState, ProbeEvidence, match_selected_profile
from core.profiles import ProfileId


def test_no_device_stops():
    result = match_selected_profile(ProfileId.JC11S, [])
    assert result.state == DetectionState.NO_DEVICE
    assert not result.may_continue


def test_wrong_transport_never_switches_profile():
    result = match_selected_profile(ProfileId.JC11S, [ProbeEvidence("imbupd", 1)])
    assert result.state == DetectionState.WRONG_PROFILE
    assert "Prime 4 G2" in result.message


def test_transport_presence_is_not_identity():
    result = match_selected_profile(ProfileId.JP24, [ProbeEvidence("imbupd", 1)])
    assert result.state == DetectionState.UNVERIFIED
    assert not result.may_continue


def test_ambiguous_devices_stop():
    result = match_selected_profile(ProfileId.JC11S, [ProbeEvidence("fastboot", 2)])
    assert result.state == DetectionState.AMBIGUOUS


def test_positive_matching_identity_may_continue():
    result = match_selected_profile(ProfileId.JC11S, [ProbeEvidence("fastboot", 1, ProfileId.JC11S)])
    assert result.state == DetectionState.MATCH
    assert result.may_continue
