import random
import emoji

# Random sarcastic emojis
sarcastic_emojis = [
    ":rolling_eyes:", ":unamused:", ":upside_down_face:",
    ":smirk:", ":nail_care:", ":face_with_raised_eyebrow:"
]

def make_sarcastic(text: str) -> str:
    """Convert text to sArCaStIc format with extra sass."""

    # Convert text to random case
    sarcastic_text = ''.join(
        c.upper() if random.random() > 0.5 else c.lower()
        for c in text
    )

    # Add random emoji
    result = f"{sarcastic_text} {emoji.emojize(random.choice(sarcastic_emojis))}"

    return result
