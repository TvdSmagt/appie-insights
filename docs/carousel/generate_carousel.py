#!/usr/bin/env python3

import io
import os
import base64
from PIL import Image

# --- Configuration ---
# Create this folder and put your screenshots inside it
IMAGE_DIR = "screenshots"
OUTPUT_FILE = "carousel.svg"

# Target width for your carousel. The height is derived from the screenshots'
# shared aspect ratio so the images are never distorted.
WIDTH = 800

# How much the screenshots' aspect ratios are allowed to differ (1% by default).
ASPECT_RATIO_TOLERANCE = 0.01

# How long each image stays on screen
SECONDS_PER_SLIDE = 3

# Encode frames as WebP (much smaller) instead of PNG. Set to False if you need
# the SVG to render in environments without WebP support.
USE_WEBP = True
# Lossy WebP quality (0-100). Only used when USE_WEBP is True.
WEBP_QUALITY = 80
# ---------------------


def determine_aspect_ratio(paths):
    """Return the shared aspect ratio (width / height) of all images.

    Raises ValueError if the aspect ratios deviate beyond the tolerance.
    """
    ratios = {}
    for path in paths:
        with Image.open(path) as img:
            ratios[path] = img.width / img.height

    reference_path, reference_ratio = next(iter(ratios.items()))
    for path, ratio in ratios.items():
        if abs(ratio - reference_ratio) / reference_ratio > ASPECT_RATIO_TOLERANCE:
            raise ValueError(
                "Screenshots have inconsistent aspect ratios:\n"
                f"  {os.path.basename(reference_path)}: {reference_ratio:.4f}\n"
                f"  {os.path.basename(path)}: {ratio:.4f}\n"
                "All screenshots must share the same aspect ratio."
            )

    return reference_ratio


def create_svg_carousel():
    # 1. Ensure the directory exists
    if not os.path.exists(IMAGE_DIR):
        print(
            f"Error: Could not find the folder '{IMAGE_DIR}'. Please create it and add some images."
        )
        return

    # 2. Find all images in the folder
    valid_extensions = (".png", ".jpg", ".jpeg", ".webp")
    image_files = [
        f for f in os.listdir(IMAGE_DIR) if f.lower().endswith(valid_extensions)
    ]
    image_files.sort()  # Alphabetical order determines the slide order

    num_images = len(image_files)
    if num_images == 0:
        print(f"Error: No images found in '{IMAGE_DIR}'.")
        return

    print(f"Found {num_images} images. Processing...")

    paths = [os.path.join(IMAGE_DIR, f) for f in image_files]

    # Derive the carousel height from the screenshots' shared aspect ratio.
    aspect_ratio = determine_aspect_ratio(paths)
    height = round(WIDTH / aspect_ratio)
    print(f"Using aspect ratio {aspect_ratio:.4f} -> {WIDTH}x{height}")

    total_time = num_images * SECONDS_PER_SLIDE
    base64_images = []

    # Pick the encoding: WebP is far smaller, PNG is the lossless fallback.
    if USE_WEBP:
        img_format, mime, save_kwargs = "WEBP", "image/webp", {"quality": WEBP_QUALITY}
    else:
        img_format, mime, save_kwargs = "PNG", "image/png", {"optimize": True}

    # 3. Process each image (Resize & Encode)
    for path in paths:
        with Image.open(path) as img:
            # Resize image to uniform dimensions
            img = img.resize((WIDTH, height), Image.Resampling.LANCZOS)

            # Save to a temporary buffer in the chosen format
            buffer = io.BytesIO()
            img.save(buffer, format=img_format, **save_kwargs)

            # Convert to Base64
            b64_str = base64.b64encode(buffer.getvalue()).decode("utf-8")
            base64_images.append(f"data:{mime};base64,{b64_str}")

    # 4. Generate the dynamic CSS
    # The slides are laid out in a single horizontal strip; we slide the whole
    # strip one image-width at a time. Each frame dwells on screen for most of
    # its time slot (HOLD_FRACTION) and then eases across to the next one.
    HOLD_FRACTION = 0.8

    keyframes = ["        0% { transform: translateX(0); }"]
    for i in range(num_images):
        start_pct = (i / num_images) * 100
        hold_pct = ((i + HOLD_FRACTION) / num_images) * 100
        offset = -i * WIDTH
        # Arrive at frame i and dwell on it.
        keyframes.append(
            f"        {start_pct:.2f}%, {hold_pct:.2f}% "
            f"{{ transform: translateX({offset}px); }}"
        )
    # End on the duplicated first frame so the loop restarts seamlessly.
    keyframes.append(
        f"        100% {{ transform: translateX({-num_images * WIDTH}px); }}"
    )
    keyframes_str = "\n".join(keyframes)

    css = f"""
      /* The strip eases between frames; ease-in-out makes each slide smooth. */
      .strip {{
        animation: slide {total_time}s ease-in-out infinite;
      }}

      @keyframes slide {{
{keyframes_str}
      }}
    """

    # 5. Build the SVG XML
    svg_content = f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 {WIDTH} {height}" width="{WIDTH}" height="{height}">\n'
    svg_content += f"  <defs>\n    <style>\n{css}\n"
    svg_content += "    </style>\n  </defs>\n\n"

    # Dark background to prevent flashing
    svg_content += f'  <rect width="{WIDTH}" height="{height}" fill="#1e1e1e" />\n\n'

    # Clip to the viewport so off-screen frames stay hidden.
    svg_content += f'  <clipPath id="viewport">\n    <rect width="{WIDTH}" height="{height}" />\n  </clipPath>\n\n'

    # Lay every frame out side by side in one strip, plus a duplicate of the
    # first frame at the end for a seamless loop.
    svg_content += '  <g clip-path="url(#viewport)">\n'
    svg_content += '    <g class="strip">\n'
    strip_images = base64_images + [base64_images[0]]
    for i, b64 in enumerate(strip_images):
        x = i * WIDTH
        svg_content += (
            f'      <image x="{x}" width="{WIDTH}" height="{height}" href="{b64}" />\n'
        )
    svg_content += "    </g>\n"
    svg_content += "  </g>\n"

    svg_content += "</svg>"

    # 6. Save the file
    with open(OUTPUT_FILE, "w") as f:
        f.write(svg_content)

    print(f"Success! '{OUTPUT_FILE}' has been generated with {num_images} slides.")


if __name__ == "__main__":
    create_svg_carousel()
