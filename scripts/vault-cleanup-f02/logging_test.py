"""Driver selection cannot turn the prerequisite into an arbitrary DB client."""
import importlib.util
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location('vault_logging', Path(__file__).with_name('logging.py'))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class LoggingProfile(unittest.TestCase):
    def test_explicit_owned_lane_and_closed_driver_profiles(self):
        self.assertEqual(module.selected_driver(['--exclusive-validation-lane']), 'postgres')
        for driver in ['postgres', 'mysql']:
            self.assertEqual(module.selected_driver(['--exclusive-validation-lane', '--driver', driver]), driver)
        for args in [[], ['--driver', 'mysql'], ['--exclusive-validation-lane', '--driver'],
                     ['--exclusive-validation-lane', '--driver', 'MySQL'],
                     ['--exclusive-validation-lane', '--driver', 'mysql', '--host', 'foreign'],
                     ['--exclusive-validation-lane', '--driver', 'mysql --host=foreign'],
                     ['--exclusive-validation-lane', '--driver', 'sqlite']]:
            with self.subTest(args=args), self.assertRaises(RuntimeError):
                module.selected_driver(args)


if __name__ == '__main__':
    unittest.main()
