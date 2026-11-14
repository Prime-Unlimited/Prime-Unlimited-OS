import unittest

from core.fastboot_probe import (
    interpret_fastboot,
    is_jc11s_candidate,
    parse_getvar_output,
)


class FastbootProbeTests(unittest.TestCase):
    def test_parse_historical_jc11s_values(self):
        output = """
(bootloader) current-slot: a
(bootloader) is-userspace: no
(bootloader) partition-size:data: 0x8fd7be00
(bootloader) partition-type:data: ext4
Finished. Total time: 0.01s
"""
        info = interpret_fastboot(output)
        self.assertEqual(info.partition_size_data, 2413280768)
        self.assertEqual(info.current_slot, "a")
        self.assertFalse(info.is_userspace)
        self.assertTrue(is_jc11s_candidate(info))

    def test_unknown_device_not_candidate(self):
        info = interpret_fastboot("(bootloader) partition-type:data: ext4")
        self.assertFalse(is_jc11s_candidate(info))

    def test_conflicting_duplicate_values_rejected(self):
        with self.assertRaises(ValueError):
            parse_getvar_output("product: JC11S\nproduct: JP24")

    def test_invalid_size_not_accepted(self):
        info = interpret_fastboot("partition-size:data: invalid")
        self.assertIsNone(info.partition_size_data)


if __name__ == "__main__":
    unittest.main()
