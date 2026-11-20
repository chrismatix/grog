from cli.app import app
from typer.testing import CliRunner


def test_convert_prints_a_sarcastic_panel():
    result = CliRunner().invoke(app, ["hello world"])

    assert result.exit_code == 0
    assert "Your sarcastic text" in result.output
    assert "hello world" not in result.output
