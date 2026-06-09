import paramiko
import sys
import time

def run_ssh_stream(client, cmd):
    print(f"\n>>> Executing remote command: {cmd}")
    transport = client.get_transport()
    channel = transport.open_session()
    channel.get_pty()
    channel.exec_command(cmd)
    
    # Read output in real-time
    while True:
        if channel.recv_ready():
            data = channel.recv(1024)
            sys.stdout.buffer.write(data)
            sys.stdout.buffer.flush()
        if channel.recv_stderr_ready():
            data_err = channel.recv_stderr(1024)
            sys.stderr.buffer.write(data_err)
            sys.stderr.buffer.flush()
        if channel.exit_status_ready():
            break
        time.sleep(0.1)
        
    status = channel.recv_exit_status()
    print(f"\n>>> Command finished with exit status: {status}")
    if status != 0:
        raise Exception(f"Command failed with exit status {status}")

def main():
    client = paramiko.SSHClient()
    client.set_missing_host_key_policy(paramiko.AutoAddPolicy())
    client.connect("192.168.6.64", username="dark", password="Aa666333!")
    
    try:
        # 1. Build the docker image
        # Using echo password | sudo -S to run docker build
        build_cmd = "cd /home/dark/tunnel-control-github && git pull && echo Aa666333! | sudo -S docker build -t darkver8/tunnel-all:latest ."
        run_ssh_stream(client, build_cmd)
        
        # 2. Push the docker image
        login_cmd = "echo Aa666333! | sudo -S sh -c 'echo Aa666333! | docker login --username darkver8 --password-stdin'"
        run_ssh_stream(client, login_cmd)
        
        push_cmd = "echo Aa666333! | sudo -S docker push darkver8/tunnel-all:latest"
        run_ssh_stream(client, push_cmd)
        
        # 3. Copy upgrade.sh to the active deployment directory
        copy_cmd = "cp /home/dark/tunnel-control-github/deploy/docker/upgrade.sh /home/dark/tunnel-control-integrated/deploy/docker/upgrade.sh"
        run_ssh_stream(client, copy_cmd)
        
        # 4. Run the upgrade script
        upgrade_cmd = "cd /home/dark/tunnel-control-integrated/deploy/docker && chmod +x upgrade.sh && echo Aa666333! | sudo -S ./upgrade.sh"
        run_ssh_stream(client, upgrade_cmd)
        
        print("\n=== DEPLOYMENT AND PUSH SUCCESSFUL ===")
    except Exception as e:
        print(f"\n!!! Deployment failed: {e}")
    finally:
        client.close()

if __name__ == "__main__":
    main()
