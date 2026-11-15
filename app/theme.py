"""Application-wide Qt theme."""

STYLESHEET = """
QWidget { background: #101419; color: #eaf2f8; font-family: Inter, Segoe UI, sans-serif; }
QMainWindow { background: #0b0f13; }
QLabel { background: transparent; }
QLabel#eyebrow { color: #29d3ff; font-weight: 700; letter-spacing: 2px; }
QLabel#title { font-size: 30px; font-weight: 700; }
QLabel#cardTitle { font-size: 23px; font-weight: 700; }
QLabel#subtitle, QLabel#muted { color: #94a5b3; font-size: 14px; }
QFrame#card { background: #171d24; border: 1px solid #26313b; border-radius: 16px; }
QFrame#card:hover { border-color: #29d3ff; background: #19232b; }
QPushButton { background: #1d2831; border: 1px solid #33424e; border-radius: 9px;
              min-height: 38px; padding: 0 18px; font-weight: 600; }
QPushButton:hover { border-color: #29d3ff; }
QPushButton#primary { background: #18bde8; color: #061117; border: 0; }
QPushButton#primary:disabled { background: #30414a; color: #71808a; }
QLineEdit, QPlainTextEdit, QComboBox { background: #0c1116; border: 1px solid #2b3944;
                           border-radius: 8px; padding: 9px; selection-background-color: #187f9c; }
QProgressBar { background: #202831; border: 0; border-radius: 4px; height: 8px; text-align: center; }
QProgressBar::chunk { background: #21c9f3; border-radius: 4px; }
QListWidget { background: transparent; border: 0; }
QListWidget::item { padding: 11px; color: #96a8b5; }
QListWidget::item:selected { background: #182832; color: #eafaff; border-radius: 7px; }
QCheckBox { spacing: 10px; }
QCheckBox::indicator { width: 18px; height: 18px; }
"""
