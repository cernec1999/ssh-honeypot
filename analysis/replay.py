#!/usr/bin/python3

# We really should port this to Golang
import termios
import sys
import tty
import os
import sqlite3
import datetime
import time
from sqlite3 import Error
from datetime import datetime


# Create the initial connection
def _create_connection(db_file):
    conn = sqlite3.connect(db_file)

    return conn


# Get the timing data for an id with the connection
def _get_timing_data_for_id(conn, id):
    cur = conn.cursor()
    cur.execute(
        f"SELECT delay, net_data FROM metadata WHERE id={id}"
    )

    rows = cur.fetchall()

    return rows


# Initialize the terminal
def _init_terminal():
    # Get the tty settings
    fd = sys.stdin.fileno()
    settings = termios.tcgetattr(fd)

    # Set the terminal to raw mode from cooked
    tty.setraw(sys.stdin)
    return settings


# Restore the terminal back to its initial state
def _restore_terminal(state):
    termios.tcsetattr(sys.stdin, termios.TCSAFLUSH, state)


# Write a raw string to the terminal
def _write_to_terminal_raw(net_data):
    os.write(sys.stdin.fileno(), net_data)


def replay_terminal_data_from_id(file, id, speedup):
    """
    Replay the terminal data from a specific connection id.

    Parameters
    ----------
    file : str
        The SQLite file
    id : int
        The connection ID
    speedup : float
        How fast to speed up the writing by
    """
    # Save current terminal state
    init_state = _init_terminal()

    # Wrap in an exception block just in case something bad happens
    try:
        # Create the initial connection to the sqlite database
        conn = _create_connection(file)

        # Get the resulting rows from the database with the given id
        resulting_rows = _get_timing_data_for_id(conn, id)

        # Iterate through the remaining timestamps
        for row in resulting_rows:
            # Sleep for n seconds, and accelerate by speedup
            time.sleep((row[0] / 1000) * (1 / speedup))

            # Write raw terminal state
            _write_to_terminal_raw(row[1])
    except Exception as e:
        print(e)
        # If we fail, restore the terminal
        _restore_terminal(init_state)
        return
    _restore_terminal(init_state)


if __name__ == "__main__":
    # Check the correct length of system arguments
    if (len(sys.argv) != 4):
        raise Error("Incorrect number of arguments!")
        exit()

    # First argument: the sqlite file
    sqlite = sys.argv[1]

    # Second argument: The connection id
    conn_id = int(sys.argv[2])

    # Third argument: The speedup time
    speedup = int(sys.argv[3])

    # Replay the terminal data
    replay_terminal_data_from_id(sqlite, conn_id, speedup)
