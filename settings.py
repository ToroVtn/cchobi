#!/usr/bin/env python3
"""cchobi's settings window (the tray's Settings…), a separate little program that only runs
while it's open, so she doesn't carry Qt around the rest of the time.

For now just her sounds, which live in ~/.cchobi: switch each one on or off, preview it (▶),
remove it (✕, moved to ~/.cchobi/removed), or add new ones. Anything added, here or by dropping
files into the folders, starts out switched on. Every change is sent to her over her socket.
"""
import json, os, re, shutil, socket, subprocess, tempfile
from pathlib import Path

from PySide6.QtWidgets import (QApplication, QDialog, QVBoxLayout, QHBoxLayout, QGroupBox, QCheckBox,
                               QPushButton, QFileDialog, QWidget)

HERE = Path(__file__).resolve().parent
SOCK = Path(os.environ.get('XDG_RUNTIME_DIR', tempfile.gettempdir())) / 'cchobi.sock'
DATA = Path.home() / '.cchobi'
DEFAULTS = HERE / 'sounds'
SOUNDS = {'meows': 'Meows', 'purrs': 'Purrs', 'water': 'Watering the plant'}


def tell(message):
    """Sends her a change (she saves it)."""
    try:
        with socket.socket(socket.AF_UNIX) as s:
            s.connect(str(SOCK))
            s.sendall(json.dumps(message).encode() + b'\n')
    except OSError:
        pass


class Settings(QDialog):
    def __init__(self):
        super().__init__()
        self.setWindowTitle('cchobi: settings')
        try:
            muted = json.loads((DATA / 'state.json').read_text()).get('muted', {})
        except (OSError, ValueError):
            muted = {}
        self.muted = {k: set(muted.get(k) or []) for k in SOUNDS}
        self.page = QVBoxLayout(self)
        self.content = None
        folder = QPushButton('Open sound folder')
        folder.clicked.connect(lambda: subprocess.Popen(['xdg-open', str(DATA)]))
        reset = QPushButton('Reset to default sounds')
        reset.clicked.connect(self.reset)
        close = QPushButton('Close')
        close.clicked.connect(self.close)
        for button in (folder, reset, close):
            self.page.addWidget(button)
        self.build()

    def build(self):
        """(Re)draws the list of sounds, so it always matches what's in the folders."""
        if self.content:
            self.content.deleteLater()
        self.content = QWidget()
        groups = QVBoxLayout(self.content)
        groups.setContentsMargins(0, 0, 0, 0)
        for key, title in SOUNDS.items():
            box = QGroupBox(title)
            rows = QVBoxLayout(box)
            for f in sorted((DATA / key).glob('*.wav'), key=lambda f: [int(t) if t.isdigit() else t
                                                                      for t in re.split(r'(\d+)', f.stem)]):
                row = QHBoxLayout()
                check = QCheckBox(f.stem)
                check.setChecked(f.name not in self.muted[key])
                check.toggled.connect(lambda on, key=key, name=f.name: self.toggle(key, name, on))
                row.addWidget(check)
                row.addStretch()
                for label, tip, action in (('▶', 'Listen', lambda _=False, f=f: subprocess.Popen(
                                                ['paplay', str(f)], stderr=subprocess.DEVNULL)),
                                           ('✕', 'Remove', lambda _=False, key=key, f=f: self.remove(key, f))):
                    button = QPushButton(label)
                    button.setFixedWidth(36)
                    button.setToolTip(tip)
                    button.clicked.connect(action)
                    row.addWidget(button)
                rows.addLayout(row)
            add = QPushButton('Add sound…')
            add.clicked.connect(lambda _=False, key=key: self.add(key))
            rows.addWidget(add)
            groups.addWidget(box)
        self.page.insertWidget(0, self.content)

    def save(self, sounds_changed=False):
        tell({'cchobi': 'muted', 'muted': {k: sorted(v) for k, v in self.muted.items()}})
        if sounds_changed:  # she picks up added or removed sounds straight away
            tell({'cchobi': 'sounds'})

    def toggle(self, key, name, on):
        (self.muted[key].discard if on else self.muted[key].add)(name)
        self.save()

    def remove(self, key, f, refresh=True):
        """Moves a sound into ~/.cchobi/removed/<kind>/, so nothing's ever lost."""
        bin_ = DATA / 'removed' / key
        bin_.mkdir(parents=True, exist_ok=True)
        f.rename(bin_ / f.name)
        self.muted[key].discard(f.name)
        if refresh:
            self.save(sounds_changed=True)
            self.build()

    def reset(self):
        """Back to exactly her default sounds, all switched on. Anything else is moved to
        ~/.cchobi/removed, like ✕ does."""
        for key in SOUNDS:
            folder = DATA / key
            defaults = {f.name for f in (DEFAULTS / key).glob('*.wav')}
            for f in list(folder.glob('*.wav')):
                if f.name not in defaults:
                    self.remove(key, f, refresh=False)
            for name in defaults - {f.name for f in folder.glob('*.wav')}:
                shutil.copy(DEFAULTS / key / name, folder / name)
            self.muted[key].clear()
        self.save(sounds_changed=True)
        self.build()

    def add(self, key):
        """Copies the sounds you pick into her folder, as .wav (videos are fine: just the audio)."""
        folder = DATA / key
        files, _ = QFileDialog.getOpenFileNames(self, 'Add sounds', str(Path.home()),
                                                'Sounds and videos (*.wav *.mp3 *.ogg *.flac *.m4a *.opus '
                                                '*.mp4 *.mkv *.webm *.mov);;Anything (*)')
        for src in map(Path, files):
            out, n = folder / f'{src.stem}.wav', 2
            while out.exists():                     # don't overwrite one that's already there
                out, n = folder / f'{src.stem}-{n}.wav', n + 1
            subprocess.run(['ffmpeg', '-v', 'error', '-i', str(src), '-vn', '-ac', '1', str(out)])
        if files:
            self.save(sounds_changed=True)
            self.build()


if __name__ == '__main__':
    app = QApplication([])
    app.setApplicationName('cchobi')
    Settings().exec()
