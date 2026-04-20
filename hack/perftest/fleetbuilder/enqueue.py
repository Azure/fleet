import os
from azure.identity import DefaultAzureCredential
from azure.storage.queue import QueueClient

STORAGE_ACCOUNT_NAME = os.getenv("STORAGE_ACCOUNT_NAME")
if not STORAGE_ACCOUNT_NAME or len(STORAGE_ACCOUNT_NAME) == 0:
    raise Exception("Missing environment variable: STORAGE_ACCOUNT_NAME")

QUEUE_NAME = os.getenv("QUEUE_NAME")
if not QUEUE_NAME or len(QUEUE_NAME) == 0:
    raise Exception("Missing environment variable: QUEUE_NAME")

START_IDX = os.getenv("START_IDX")
if not START_IDX or len(START_IDX) == 0 or not START_IDX.isnumeric():
    raise Exception("Missing or invalid environment variable: START_IDX")

END_IDX = os.getenv("END_IDX")
if not END_IDX or len(END_IDX) == 0 or not END_IDX.isnumeric():
    raise Exception("Missing or invalid environment variable: END_IDX")

if int(START_IDX) > int(END_IDX):
    raise Exception("Invalid environment variables: START_IDX should be less than or equal to END_IDX")

account_url = f"https://{STORAGE_ACCOUNT_NAME}.queue.core.windows.net"
default_cred = DefaultAzureCredential()
queue_client = QueueClient(account_url=account_url,
                           queue_name=QUEUE_NAME,
                           credential=default_cred)

for i in range(int(START_IDX), int(END_IDX) + 1):
    queue_client.send_message(f"{i}")
