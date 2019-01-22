package lepton

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type drive struct {
	path   string
	format string
}

type netdev struct {
	nettype string
	id      string
	ifname  string
	mac     string
}

type qemu struct {
	cmd    *exec.Cmd
	drives []drive
	ifaces []netdev
}

func (q *qemu) addDrive(path string, format string) {

}

func (q *qemu) addNetworkDevice(n netdev) {

}

func (q *qemu) Stop() {
	if q.cmd != nil {
		q.cmd.Process.Kill()
	}
}
func logv(rconfig *RunConfig, msg string) {
	if rconfig.Verbose {
		fmt.Println(msg)
	}
}

func (q *qemu) Command(rconfig *RunConfig) *exec.Cmd {
	args := q.Args(rconfig)
	q.cmd = exec.Command("qemu-system-x86_64", args...)
	return q.cmd
}

func (q *qemu) Start(rconfig *RunConfig) error {
	args := q.Args(rconfig)
	logv(rconfig, "qemu-system-x86_64 "+strings.Join(args, " "))
	q.cmd = exec.Command("qemu-system-x86_64", args...)
	q.cmd.Stdout = os.Stdout
	q.cmd.Stdin = os.Stdin
	q.cmd.Stderr = os.Stderr

	if err := q.cmd.Run(); err != nil {
		fmt.Println(err)
		return err
	}
	return nil
}

func (q *qemu) Args(rconfig *RunConfig) []string {
	// TODO : this should come from q.drives and ifaces
	args := []string{}

	boot := []string{"-drive", "file=image,format=raw,index=0"}
	storage := []string{"-drive", "file=image,format=raw,if=virtio"}
	var net []string
	if rconfig.Bridged {
		net = []string{"-device", "virtio-net,mac=7e:b8:7e:87:4a:ea,netdev=n0",
			"-netdev", "tap,id=n0,ifname=tap0,script=no,downscript=no"}

	} else {
		// hostfwd=tcp::8080-:8080
		portfw := []string{}
		for _, port := range rconfig.Ports {
			portfw = append(portfw,
				fmt.Sprintf("hostfwd=tcp::%v-:%v", port, port))
		}
		net = []string{"-device", "virtio-net,netdev=n0", "-netdev",
			"user,id=n0," + strings.Join(portfw, ",")}
	}
	display := []string{"-display", "none", "-serial", "stdio"}
	args = append(args, boot...)
	args = append(args, display...)
	args = append(args, []string{"-nodefaults", "-no-reboot", "-m", rconfig.Memory, "-device", "isa-debug-exit"}...)
	args = append(args, storage...)
	args = append(args, net...)
	if rconfig.Bridged {
		args = append(args, "-enable-kvm")
	}
	return args
}

func newQemu() Hypervisor {
	return &qemu{}
}
