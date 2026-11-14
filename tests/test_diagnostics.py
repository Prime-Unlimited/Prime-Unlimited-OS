from core.diagnostics import redact


def test_log_redaction_hides_common_secrets():
    output = redact("password=secret token: abc passphrase = words")
    assert "secret" not in output
    assert "abc" not in output
    assert "words" not in output


def test_private_key_block_is_removed():
    output = redact("before -----BEGIN OPENSSH PRIVATE KEY-----\nsecret\n-----END OPENSSH PRIVATE KEY----- after")
    assert "secret" not in output
    assert "PRIVATE KEY REDACTED" in output
