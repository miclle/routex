"""F02 depends on genuine complete normal proof and preserves unknown evidence."""
import copy
import importlib.util
from pathlib import Path
import tempfile
import unittest

spec = importlib.util.spec_from_file_location('f02', Path(__file__).with_name('run.py'))
f02 = importlib.util.module_from_spec(spec)
spec.loader.exec_module(f02)


class Prerequisite(unittest.TestCase):
    def test_partial_foreign_driver_and_uncertain_proof_refuse(self):
        original = dict(accepted=True, driver='postgres', source_unchanged=True,
                        original_containers_network_image_binaries_absent=True,
                        native_files=29, preflight_files=18, paired_audit_calls=5,
                        observer_Cookie_headers=[False, False, False], F02=False,
                        global_completion=False, host_uid=501, run='a' * 32,
                        source='b' * 40, source_manifest='c' * 64, raw_audit_sha256='d' * 64)
        self.assertIs(f02.normal_prerequisite(original, 501), original)
        changes = dict(accepted=False, driver='mysql', source_unchanged=False,
                       original_containers_network_image_binaries_absent=False,
                       native_files=28, preflight_files=17, paired_audit_calls=4,
                       observer_Cookie_headers=[False, True, False], F02=True,
                       global_completion=True, host_uid=502, run='', source='',
                       source_manifest='', raw_audit_sha256='')
        for key, value in changes.items():
            candidate = copy.deepcopy(original)
            candidate[key] = value
            with self.subTest(key=key), self.assertRaises(RuntimeError):
                f02.normal_prerequisite(candidate, 501)
        with self.assertRaises(RuntimeError):
            f02.normal_prerequisite({}, 501)

    def test_driver_profiles_and_receipts_cannot_cross(self):
        args = ['--exclusive-validation-lane', '--normal-receipt', '/private/normal',
                '--normal-receipt-sha256', 'a'*64, '--logging-receipt', '/private/logging',
                '--logging-receipt-sha256', 'b'*64]
        self.assertEqual(f02.selected_driver(args), 'postgres')
        self.assertEqual(f02.selected_driver(args+['--driver','mysql']), 'mysql')
        for extra in [[], ['--driver','MySQL'], ['--driver','mysql','--host','foreign']]:
            bad = [] if not extra else args+extra
            with self.assertRaises(RuntimeError): f02.selected_driver(bad)
        safe = dict(status='PASS_ACTUAL_PRIVATE_MYSQL84_TYPED_REFUSAL_AND_CHECKED_ROLLBACK',
                    driver='mysql', whole_original_append_count=0, original_append_bytes=0,
                    raw_append_sha256=f02.hashlib.sha256(b'').hexdigest(),
                    private_marker_and_password_absent=True, product_cleanup=False,
                    F02=False, global_completion=False)
        self.assertIs(f02.logging_prerequisite(safe,'mysql'),safe)
        for key,value in [('driver','postgres'),('original_append_bytes',1),('whole_original_append_count',1),('raw_append_sha256','a'*64),('F02',True),('private_marker_and_password_absent',False)]:
            bad = dict(safe);bad[key]=value
            with self.subTest(key=key),self.assertRaises(RuntimeError): f02.logging_prerequisite(bad,'mysql')
        with self.assertRaises(RuntimeError): f02.logging_prerequisite(safe,'postgres')

    def test_unknown_artifact_prevents_retirement(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            artifacts = root / 'artifacts'
            artifacts.mkdir()
            binary = artifacts / 'f02-client'
            binary.write_bytes(b'fresh')
            expected = {'f02-client': f02.sha(binary)}
            (artifacts / 'unknown-receipt').write_bytes(b'preserve')
            with self.assertRaises(RuntimeError):
                f02.retire_artifacts(root, expected)
            self.assertTrue(binary.exists())


if __name__ == '__main__':
    unittest.main()
