import os
from azure.identity import DefaultAzureCredential
from azure.storage.queue import QueueClient

STORAGE_ACCOUNT_NAME = os.getenv("STORAGE_ACCOUNT_NAME")
if not STORAGE_ACCOUNT_NAME or len(STORAGE_ACCOUNT_NAME) == 0:
    raise Exception("Missing environment variable: STORAGE_ACCOUNT_NAME")

QUEUE_NAME = os.getenv("QUEUE_NAME")
if not QUEUE_NAME or len(QUEUE_NAME) == 0:
    raise Exception("Missing environment variable: QUEUE_NAME")

visibility_timeout = 600 # 10 minutes

account_url= f"https://{STORAGE_ACCOUNT_NAME}.queue.core.windows.net"
default_cred = DefaultAzureCredential()
queue_client = QueueClient(account_url=account_url,
                           queue_name=QUEUE_NAME,
                           credential=default_cred)

received_msgs = queue_client.receive_messages(max_messages=1, visibility_timeout=visibility_timeout)
for msg in received_msgs:
    print(f"{msg.content}")
    queue_client.delete_message(msg)
