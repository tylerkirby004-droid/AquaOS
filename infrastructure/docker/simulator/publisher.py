"""Dependency-free MQTT 3.1.1 telemetry publisher for local AquaOS development."""
import json
import os
import random
import socket
import struct
import time
from datetime import datetime, timezone

HOST = os.getenv("MQTT_HOST", "localhost")
USERNAME = os.getenv("MQTT_USERNAME", "reefpi")
PASSWORD = os.getenv("MQTT_PASSWORD", "change-me-before-deploying")


def encoded_length(value):
    output = bytearray()
    while True:
        digit = value % 128
        value //= 128
        output.append(digit | (128 if value else 0))
        if not value:
            return bytes(output)


def mqtt_string(value):
    data = value.encode()
    return struct.pack("!H", len(data)) + data


def connect():
    client_id = f"aquaos-simulator-{random.randrange(1_000_000)}"
    flags = 0xC2  # username, password, clean session
    variable_header = mqtt_string("MQTT") + bytes([4, flags]) + struct.pack("!H", 60)
    payload = mqtt_string(client_id) + mqtt_string(USERNAME) + mqtt_string(PASSWORD)
    packet = bytes([0x10]) + encoded_length(len(variable_header) + len(payload)) + variable_header + payload
    sock = socket.create_connection((HOST, 1883), timeout=10)
    sock.sendall(packet)
    response = sock.recv(4)
    if response != b" \x02\x00\x00":
        raise ConnectionError(f"MQTT connection refused: {response!r}")
    return sock


def publish(sock, sensor, value, unit):
    topic = f"aquaos/home/reef/sensor/{sensor}/value"
    payload = json.dumps({"value": round(value, 2), "unit": unit, "timestamp": datetime.now(timezone.utc).isoformat(), "source": "simulator"})
    packet_id = random.randrange(1, 65536)
    body = mqtt_string(topic) + struct.pack("!H", packet_id) + payload.encode()
    sock.sendall(bytes([0x32]) + encoded_length(len(body)) + body)  # QoS 1, retained
    sock.recv(4)  # PUBACK


while True:
    try:
        with connect() as client:
            while True:
                now = time.time()
                publish(client, "temperature", 78.2 + 0.6 * __import__("math").sin(now / 300) + random.uniform(-0.1, 0.1), "F")
                publish(client, "ph", 8.15 + 0.08 * __import__("math").sin(now / 500) + random.uniform(-0.02, 0.02), "pH")
                publish(client, "salinity", 35.0 + random.uniform(-0.08, 0.08), "ppt")
                time.sleep(15)
    except (OSError, ConnectionError):
        time.sleep(5)
