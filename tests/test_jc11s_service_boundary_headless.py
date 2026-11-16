"""Exercise the actual Qt workflow methods headlessly, without importing PySide6."""
import ast
from pathlib import Path
from types import SimpleNamespace
from unittest import TestCase
from unittest.mock import Mock

from core.jc11s_device_detection import DetectionResult, DetectionState
from core.jc11s_diagnostic_service import DiagnosticReport, UsbDiagnosticService
from core.profiles import ProfileId


def workflow_method(name, namespace):
    source = Path(__file__).resolve().parents[1] / 'app' / 'jc11s_window.py'
    tree = ast.parse(source.read_text(encoding='utf-8'))
    cls = next(n for n in tree.body if isinstance(n, ast.ClassDef) and n.name == 'WorkflowPage')
    method = next(n for n in cls.body if isinstance(n, ast.FunctionDef) and n.name == name)
    method.decorator_list = []
    module = ast.fix_missing_locations(ast.Module(body=[method], type_ignores=[]))
    exec(compile(module, str(source), 'exec'), namespace)
    return namespace[name]


class ServiceBoundaryTests(TestCase):
    def setUp(self):
        self.created = []
        def installer(transport):
            instance = SimpleNamespace(fastboot=transport, prepare=Mock())
            self.created.append(instance)
            return instance
        self.ns = {'ProfileId': ProfileId, 'DetectionState': DetectionState, 'DiagnosticReport': DiagnosticReport,
                   'Jc11sInstallerService': installer, 'redact': lambda x: x,
                   'TaskWorker': lambda fn: SimpleNamespace(signals=SimpleNamespace(finished=Mock(), failed=Mock()))}
        self.on_report = workflow_method('handle_diagnostic_report', self.ns)
        self.prepare = workflow_method('prepare_image', self.ns)
        self.fastboot = Mock()
        self.fastboot.collect_partition_evidence = Mock()
        self.diagnostic = UsbDiagnosticService()
        self.wizard = SimpleNamespace(
            profile_id=ProfileId.JC11S, diagnostic_service=self.diagnostic,
            installer_service=None, key_pair=None, diagnostic_report=None,
            run_diagnostics_button=Mock(), diagnostic_log=Mock(), diagnostic_status=Mock(),
            reboot_button=Mock(), build_status=Mock(), build_progress=Mock(),
            prepare_button=Mock(), candidate_selector=Mock(), thread_pool=Mock(),
            _image_ready=Mock(), _image_failed=Mock(),
            _populate_candidates=Mock(), _update_navigation=Mock(),
        )
        self.wizard.candidate_selector.isVisible.return_value = False
        self.wizard.candidate_selector.currentData.return_value = None

    def report(self, success=True):
        state = DetectionState.MATCH if success else DetectionState.NO_DEVICE
        return DiagnosticReport(DetectionResult(state, 'diagnostic result', success), ())

    def test_service_is_created_after_successful_diagnostics_once(self):
        self.assertIsNone(self.wizard.installer_service)
        self.assertIsNone(self.diagnostic.fastboot)
        self.diagnostic.fastboot = self.fastboot  # normal probe's lazy transport creation
        self.on_report(self.wizard, self.report())
        self.assertIsNotNone(self.wizard.installer_service, 'validated USB diagnostics left installer service missing')
        self.assertIs(self.wizard.installer_service.fastboot, self.fastboot)
        self.on_report(self.wizard, self.report())
        self.assertEqual(len(self.created), 1)

    def test_missing_or_unsupported_transport_stays_blocked(self):
        for transport in (None, object()):
            self.diagnostic.fastboot = transport
            self.on_report(self.wizard, self.report())
            self.assertIsNone(self.wizard.installer_service)
        self.assertFalse(self.created)

    def test_failed_diagnostics_does_not_create_service(self):
        self.diagnostic.fastboot = self.fastboot
        self.on_report(self.wizard, self.report(False))
        self.assertIsNone(self.wizard.installer_service)

    def test_jp24_does_not_create_jc11s_service(self):
        self.wizard.profile_id = ProfileId.JP24
        self.diagnostic.fastboot = self.fastboot
        self.on_report(self.wizard, self.report())
        self.assertIsNone(self.wizard.installer_service)

    def test_key_is_still_mandatory_and_missing_service_blocks_worker(self):
        self.diagnostic.fastboot = self.fastboot
        self.on_report(self.wizard, self.report())
        self.prepare(self.wizard)
        self.wizard.thread_pool.start.assert_not_called()
        self.wizard.build_status.setText.assert_called_with(
            'JC11S installer service and a validated SSH key are required.')
        self.wizard.key_pair = SimpleNamespace(public_key='safe-test-public-key')
        self.prepare(self.wizard)
        self.wizard.thread_pool.start.assert_called_once()
        self.assertIsNotNone(self.wizard.installer_service)

    def test_missing_service_blocks_even_with_key(self):
        self.wizard.key_pair = SimpleNamespace(public_key='safe-test-public-key')
        self.prepare(self.wizard)
        self.wizard.thread_pool.start.assert_not_called()
