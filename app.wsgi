#!/usr/bin/python3
import sys
import os

# Add your project directory to the sys.path
project_home = '/path/to/desktime/tracker'
if project_home not in sys.path:
    sys.path.insert(0, project_home)

# Change to project directory
os.chdir(project_home)

# Import the Flask app
from server import app as application
