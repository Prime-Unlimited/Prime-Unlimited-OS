"""Application-wide Qt theme, matching the Prime Unlimited OS brand:
near-black background, orange accent, cream text, slate-gray muted text,
Archivo Narrow (closest bundled match to Archivo Condensed - Qt does not
expose the Archivo variable font's width axis as a selectable family) for
display headings, IBM Plex Mono for eyebrow labels and technical text.
"""

BG = "#0E0F11"
CARD_BG = "#17191C"
CARD_BORDER = "#2A2D31"
ORANGE = "#FF5A1F"
CREAM = "#F2F0EA"
SLATE = "#5B616B"

DISPLAY_FONT = "Archivo Narrow"
MONO_FONT = "IBM Plex Mono"

STYLESHEET = f"""
QWidget {{ background: {BG}; color: {CREAM}; font-family: "{MONO_FONT}", Consolas, monospace; font-size: 13px; }}
QMainWindow {{ background: {BG}; }}

QLabel#eyebrow {{
    color: {ORANGE}; font-family: "{MONO_FONT}", monospace; font-weight: 500;
    letter-spacing: 3px; text-transform: uppercase; font-size: 12px;
}}
QLabel#brand-title {{
    color: {CREAM}; font-family: "{DISPLAY_FONT}", sans-serif; font-weight: 700;
    font-size: 22px; letter-spacing: 1px;
}}
QLabel#brand-subtitle {{
    color: {ORANGE}; font-family: "{MONO_FONT}", monospace; font-weight: 500;
    letter-spacing: 4px; font-size: 11px;
}}
QLabel#title {{
    color: {CREAM}; font-family: "{DISPLAY_FONT}", sans-serif; font-weight: 700;
    font-size: 34px; letter-spacing: 1px;
}}
QLabel#subtitle {{ color: {CREAM}; font-size: 14px; }}
QLabel#muted {{ color: {SLATE}; font-size: 13px; }}

QFrame#card {{ background: {CARD_BG}; border: 1px solid {CARD_BORDER}; border-radius: 14px; }}
QFrame#card:hover {{ border-color: {ORANGE}; }}

QPushButton {{
    background: {CARD_BG}; color: {CREAM}; border: 1px solid {CARD_BORDER}; border-radius: 8px;
    min-height: 38px; padding: 0 18px; font-family: "{MONO_FONT}", monospace;
    font-weight: 500; letter-spacing: 0.5px;
}}
QPushButton:hover {{ border-color: {ORANGE}; color: {ORANGE}; }}
QPushButton:disabled {{ color: {SLATE}; border-color: {CARD_BORDER}; }}
QPushButton#primary {{ background: {ORANGE}; color: {BG}; border: 0; font-weight: 700; }}
QPushButton#primary:hover {{ background: #ff7238; color: {BG}; }}
QPushButton#primary:disabled {{ background: #4a3426; color: #8a7363; }}

QLineEdit, QPlainTextEdit {{
    background: {BG}; color: {CREAM}; border: 1px solid {CARD_BORDER}; border-radius: 6px;
    padding: 9px; font-family: "{MONO_FONT}", monospace; selection-background-color: {ORANGE};
    selection-color: {BG};
}}
QLineEdit:focus, QPlainTextEdit:focus {{ border-color: {ORANGE}; }}

QProgressBar {{ background: {CARD_BORDER}; border: 0; border-radius: 4px; height: 8px; text-align: center; }}
QProgressBar::chunk {{ background: {ORANGE}; border-radius: 4px; }}

QListWidget {{ background: transparent; border: 0; font-family: "{MONO_FONT}", monospace; }}
QListWidget::item {{ padding: 11px; color: {SLATE}; }}
QListWidget::item:selected {{ background: {CARD_BG}; color: {ORANGE}; border-radius: 7px; }}

QCheckBox {{ spacing: 10px; }}
QCheckBox::indicator {{ width: 18px; height: 18px; }}

QMessageBox {{ background: {CARD_BG}; }}
"""
