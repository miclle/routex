"""Owned artifact retirement must never discard unknown files or mismatched binaries."""
import importlib.util
from pathlib import Path
import tempfile
import unittest

spec = importlib.util.spec_from_file_location('normal', Path(__file__).with_name('vault-cleanup-normal.py'))
normal = importlib.util.module_from_spec(spec)
spec.loader.exec_module(normal)


class ArtifactRetirement(unittest.TestCase):
    def test_only_exact_checked_binaries_are_retired(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            artifacts = root / 'artifacts'
            artifacts.mkdir()
            binary = artifacts / 'routex'
            binary.write_bytes(b'fresh-run-binary')
            receipt = normal.retire_artifacts(root, {'routex': normal.sha(binary)})
            self.assertTrue(receipt['after_verified_closure'])
            self.assertFalse(artifacts.exists())

    def test_unknown_evidence_and_changed_binary_are_preserved(self):
        for failure in ['unknown', 'changed', 'symlink']:
            with self.subTest(failure=failure), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                artifacts = root / 'artifacts'
                artifacts.mkdir()
                binary = artifacts / 'routex'
                binary.write_bytes(b'original')
                expected = {'routex': normal.sha(binary)}
                if failure == 'unknown':
                    (artifacts / 'unknown-receipt').write_bytes(b'preserve')
                elif failure == 'changed':
                    binary.write_bytes(b'changed')
                else:
                    other = root / 'other'
                    other.write_bytes(b'original')
                    binary.unlink()
                    binary.symlink_to(other)
                with self.assertRaises(RuntimeError):
                    normal.retire_artifacts(root, expected)
                self.assertTrue(artifacts.exists())
                self.assertTrue(binary.exists())


class DatabaseProfile(unittest.TestCase):
    def test_only_exact_reserved_profiles_are_accepted(self):
        self.assertEqual(normal.parse_driver(['--exclusive-validation-lane']), 'postgres')
        for driver in ['postgres', 'mysql']:
            self.assertEqual(normal.parse_driver(['--exclusive-validation-lane', '--driver', driver]), driver)
        for args in [[], ['--driver', 'mysql'], ['--exclusive-validation-lane', '--driver', 'MySQL'], ['--exclusive-validation-lane', '--driver', 'sqlite'], ['--exclusive-validation-lane', '--driver', 'mysql', '--host', 'foreign']]:
            with self.subTest(args=args), self.assertRaises(RuntimeError):
                normal.parse_driver(args)


if __name__ == '__main__':
    unittest.main()
