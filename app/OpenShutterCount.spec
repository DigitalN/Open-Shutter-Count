# PyInstaller spec for Open Shutter Count. Built by app/scripts/build.sh, which then puts
# the openshuttercount tool and libusb (tool/mac/) in Contents/MacOS, side by side, and signs
# the whole app.

import os
import re

# The version lives in version.py only.
with open(os.path.join(SPECPATH, 'version.py'), encoding='utf-8') as f:
    VERSION = re.search(r'^VERSION = "([^"]+)"', f.read(), re.MULTILINE).group(1)

# build.sh draws the icon (scripts/make_icon.py) and says where it is.
ICON_PATH = os.environ.get('OPENSHUTTERCOUNT_ICON', '')

a = Analysis(
    ['main.py'],
    pathex=[SPECPATH],
    binaries=[],
    datas=[
        ('index.html', '.'),
        # Open Shutter Count's license and the licenses of what's inside: Contents/Resources
        # in the app.
        (os.path.join(SPECPATH, '..', 'LICENSE'), '.'),
        (os.path.join(SPECPATH, '..', 'docs', 'THIRD-PARTY-NOTICES'), '.'),
    ],
    hiddenimports=[
        'webview',
        'webview.platforms.cocoa',
        'objc',
        'Foundation',
        'AppKit',
        'WebKit',
        'PyObjCTools.AppHelper',
    ],
    hookspath=[],
    runtime_hooks=[],
    # Other pywebview backends and unused stdlib GUI toolkits.
    excludes=[
        'tkinter',
        'webview.platforms.android',
        'webview.platforms.cef',
        'webview.platforms.edgechromium',
        'webview.platforms.gtk',
        'webview.platforms.mshtml',
        'webview.platforms.qt',
        'webview.platforms.winforms',
    ],
    noarchive=False,
)

pyz = PYZ(a.pure, a.zipped_data)

exe = EXE(
    pyz,
    a.scripts,
    [],
    exclude_binaries=True,
    name='Open Shutter Count',
    debug=False,
    strip=False,
    upx=False,
    console=False,
    target_arch='arm64',
)

coll = COLLECT(
    exe,
    a.binaries,
    a.datas,
    strip=False,
    upx=False,
    name='Open Shutter Count',
)

app = BUNDLE(
    coll,
    name='Open Shutter Count.app',
    icon=ICON_PATH if os.path.exists(ICON_PATH) else None,
    bundle_identifier='com.digitaln.openshuttercount',
    info_plist={
        'CFBundleName': 'Open Shutter Count',
        'CFBundleDisplayName': 'Open Shutter Count',
        'CFBundleVersion': VERSION,
        'CFBundleShortVersionString': VERSION,
        'LSMinimumSystemVersion': '12.0',
        'LSArchitecturePriority': ['arm64'],
        'NSHighResolutionCapable': True,
        'NSHumanReadableCopyright': 'Copyright © 2026 DigitalN. GNU GPL v3 or later.',
    },
)
