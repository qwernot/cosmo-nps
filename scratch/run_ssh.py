import paramiko
import sys

client = paramiko.SSHClient()
client.set_missing_host_key_policy(paramiko.AutoAddPolicy())
client.connect("192.168.6.64", username="dark", password="Aa666333!")

cmd = sys.argv[1] if len(sys.argv) > 1 else "ls -la"
stdin, stdout, stderr = client.exec_command(cmd)

print("--- STDOUT ---")
print(stdout.read().decode('utf-8'))
print("--- STDERR ---")
print(stderr.read().decode('utf-8'))

client.close()
