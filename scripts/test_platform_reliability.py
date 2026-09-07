import pathlib
import unittest


ROOT = pathlib.Path(__file__).resolve().parents[1]


def read(relative_path: str) -> str:
    return (ROOT / relative_path).read_text(encoding="utf-8")


class PlatformReliabilityContractTest(unittest.TestCase):
    def test_ci_runs_live_commerce_and_image_contracts(self) -> None:
        workflow = read(".github/workflows/ci.yml")
        self.assertIn("./internal/order/", workflow)
        self.assertIn("./internal/payment/", workflow)
        self.assertIn("verify-commerce-image.sh", workflow)

    def test_monitor_opens_and_closes_an_incident(self) -> None:
        workflow = read(".github/workflows/production-monitor.yml")
        self.assertIn("issues: write", workflow)
        self.assertIn("[Sev-1] Production critical reads are failing", workflow)
        self.assertIn("gh issue create", workflow)
        self.assertIn("gh issue close", workflow)

    def test_release_verification_is_targeted_and_actionable(self) -> None:
        workflow = read(".github/workflows/production-smoke.yml")
        for required in (
            "base_url:",
            ".twc1.net",
            "FICUSIN_BASE_URL",
            "retention-days: 30",
            "[Sev-1] Production release verification failed",
        ):
            self.assertIn(required, workflow)

    def test_production_browser_suite_accepts_verified_target(self) -> None:
        config = read("e2e/playwright.production.config.ts")
        self.assertIn("process.env.FICUSIN_BASE_URL", config)


if __name__ == "__main__":
    unittest.main()
