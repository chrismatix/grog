import typer
from rich import print
from rich.panel import Panel
from rich.console import Console

from format.make_sarcastic import make_sarcastic

app = typer.Typer(
    help="Convert boring text into sArCaStIc text with style!"
)

console = Console()


@app.command()
def convert(
        text: str = typer.Argument(..., help="The text to make sarcastic"),
        rainbow: bool = typer.Option(
            False, "--rainbow", "-r",
            help="Make it rainbow colored (because why not?)"
        ),
):
    """
    Convert your boring text into something more... interesting 💅
    """
    result = make_sarcastic(text)
    display_text = result

    if rainbow:
        print(Panel(
            display_text,
            title="[bold]Your sarcastic text[/bold]",
            border_style="rainbow",
            padding=(1, 2)
        ))
    else:
        print(Panel(
            display_text,
            title="[bold]Your sarcastic text[/bold]",
            border_style="blue",
            padding=(1, 2)
        ))
