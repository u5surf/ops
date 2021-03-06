package lepton

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"github.com/olekukonko/tablewriter"
	"github.com/vmware/govmomi/find"

	"github.com/vmware/govmomi/govc/host/esxcli"
	"github.com/vmware/govmomi/object"

	"github.com/vmware/govmomi/view"
	"github.com/vmware/govmomi/vim25"
	"github.com/vmware/govmomi/vim25/methods"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/soap"
	"github.com/vmware/govmomi/vim25/types"
)

// Vsphere provides access to the Vsphere API.
type Vsphere struct {
	Storage *Datastores
	client  *vim25.Client

	datacenter   string
	datastore    string
	network      string
	resourcePool string
}

// ResizeImage is not supported on VSphere.
func (v *Vsphere) ResizeImage(ctx *Context, imagename string, hbytes string) error {
	return fmt.Errorf("Operation not supported")
}

// BuildImage to be upload on VSphere
func (v *Vsphere) BuildImage(ctx *Context) (string, error) {
	c := ctx.config
	err := BuildImage(*c)
	if err != nil {
		return "", err
	}

	return v.customizeImage(ctx)
}

// BuildImageWithPackage to upload on Vsphere.
func (v *Vsphere) BuildImageWithPackage(ctx *Context, pkgpath string) (string, error) {
	c := ctx.config
	err := BuildImageFromPackage(pkgpath, *c)
	if err != nil {
		return "", err
	}
	return v.customizeImage(ctx)
}

func (v *Vsphere) createImage(key string, bucket string, region string) {
	fmt.Println("un-implemented")
}

// Initialize Vsphere related things
func (v *Vsphere) Initialize() error {
	u, err := v.getCredentials()
	if err != nil {
		return err
	}
	// consume from env vars if set
	dc := os.Getenv("GOVC_DATACENTER")
	ds := os.Getenv("GOVC_DATASTORE")
	nw := os.Getenv("GOVC_NETWORK")
	rp := os.Getenv("GOVC_RESOURCE_POOL")

	v.datacenter = "/ha-datacenter/"
	if dc != "" {
		v.datacenter = dc
	}

	v.datastore = v.datacenter + "datastore/datastore1/"
	if ds != "" {
		v.datastore = ds
	}

	v.network = v.datacenter + "network/VM Network"
	if nw != "" {
		v.network = nw
	}

	// this can be inferred?
	v.resourcePool = v.datacenter + "host/localhost.hsd1.ca.comcast.net/Resources"
	if rp != "" {
		v.resourcePool = rp
	}

	un := u.User.Username()
	pw, _ := u.User.Password()
	soapClient := soap.NewClient(u, true)
	v.client, err = vim25.NewClient(context.Background(), soapClient)
	if err != nil {
		return err
	}

	req := types.Login{
		This: *v.client.ServiceContent.SessionManager,
	}
	req.UserName = un
	req.Password = pw

	_, err = methods.Login(context.Background(), v.client, &req)
	if err != nil {
		return err
	}

	return nil
}

// CreateImage - Creates image on vsphere using nanos images
// This merely uploads the flat and base image to the datastore and then
// creates a copy of the image to perform the vmfs translation (import
// does not do this by default). This sidesteps the vmfkstools
// transformation.
func (v *Vsphere) CreateImage(ctx *Context) error {
	vmdkBase := strings.ReplaceAll(ctx.config.CloudConfig.ImageName, "-image", "")

	flat := vmdkBase + "-flat.vmdk"
	base := vmdkBase + ".vmdk"

	flatPath := "/tmp/" + flat
	imgPath := "/tmp/" + base

	f := find.NewFinder(v.client, true)
	ds, err := f.DatastoreOrDefault(context.TODO(), v.datastore)
	if err != nil {
		fmt.Println(err)
		return err
	}

	p := soap.DefaultUpload
	ds.UploadFile(context.TODO(), flatPath, vmdkBase+"/"+flat, &p)
	ds.UploadFile(context.TODO(), imgPath, vmdkBase+"/"+base, &p)

	dc, err := f.DatacenterOrDefault(context.TODO(), v.datacenter)
	if err != nil {
		fmt.Println(err)
		return err
	}

	m := ds.NewFileManager(dc, true)

	m.Copy(context.TODO(), vmdkBase+"/"+base, vmdkBase+"/"+vmdkBase+"2.vmdk")

	return nil
}

// ListImages lists images on a datastore.
// This is incredibly naive at the moment and probably worth putting
// under a root folder.
// essentially does the equivalent of 'govc datastore.ls'
func (v *Vsphere) ListImages(ctx *Context) error {

	f := find.NewFinder(v.client, true)
	ds, err := f.DatastoreOrDefault(context.TODO(), v.datastore)
	if err != nil {
		fmt.Println(err)
		return err
	}

	b, err := ds.Browser(context.TODO())
	if err != nil {
		return err
	}

	spec := types.HostDatastoreBrowserSearchSpec{
		MatchPattern: []string{"*"},
	}

	search := b.SearchDatastore

	task, err := search(context.TODO(), ds.Path(""), &spec)
	if err != nil {
		fmt.Println(err)
	}

	info, err := task.WaitForResult(context.TODO(), nil)
	if err != nil {
		fmt.Println(err)
	}

	images := []string{}

	switch r := info.Result.(type) {
	case types.HostDatastoreBrowserSearchResults:
		res := []types.HostDatastoreBrowserSearchResults{r}
		for i := 0; i < len(res); i++ {
			for _, f := range res[i].File {
				if f.GetFileInfo().Path[0] == '.' {
					continue
				}
				images = append(images, f.GetFileInfo().Path)
			}
		}
	case types.ArrayOfHostDatastoreBrowserSearchResults:
		fmt.Println("un-implemented")
	}

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"Name", "Status", "Created"})
	table.SetHeaderColor(
		tablewriter.Colors{tablewriter.Bold, tablewriter.FgCyanColor},
		tablewriter.Colors{tablewriter.Bold, tablewriter.FgCyanColor},
		tablewriter.Colors{tablewriter.Bold, tablewriter.FgCyanColor})
	table.SetRowLine(true)

	for _, image := range images {
		var row []string
		row = append(row, image)
		row = append(row, "")
		row = append(row, "")
		table.Append(row)
	}

	table.Render()

	return nil
}

// DeleteImage deletes image from VSphere
func (v *Vsphere) DeleteImage(ctx *Context, imagename string) error {
	fmt.Println("un-implemented")
	return nil
}

// CreateInstance - Creates instance on VSphere.
// Currently we support pvsci adapter && vmnetx3 network driver.
func (v *Vsphere) CreateInstance(ctx *Context) error {

	var devices object.VirtualDeviceList
	var err error

	imgName := ctx.config.CloudConfig.ImageName

	fmt.Printf("spinning up:\t%s\n", imgName)

	spec := &types.VirtualMachineConfigSpec{
		Name:       imgName,
		GuestId:    "otherGuest64",
		NumCPUs:    1,
		MemoryMB:   1024,
		Annotation: "",
		Firmware:   string(types.GuestOsDescriptorFirmwareTypeBios),
		Version:    "",
	}

	// add disk
	scsi, err := devices.CreateSCSIController("pvscsi")
	if err != nil {
		fmt.Println(err)
	}

	devices = append(devices, scsi)
	controller := devices.Name(scsi)

	dcontroller, err := devices.FindDiskController(controller)
	if err != nil {
		fmt.Println(err)
	}

	f := find.NewFinder(v.client, true)
	ds, err := f.DatastoreOrDefault(context.TODO(), v.datastore)
	if err != nil {
		fmt.Println(err)
		return err
	}

	dpath := ds.Path(imgName + "/" + imgName + "2.vmdk")
	disk := devices.CreateDisk(dcontroller, ds.Reference(), dpath)

	disk = devices.ChildDisk(disk)

	devices = append(devices, disk)
	// end add disk

	// add network
	// infer network stub
	net, err := f.NetworkOrDefault(context.TODO(), v.network)
	if err != nil {
		fmt.Println(err)
	}

	backing, err := net.EthernetCardBackingInfo(context.TODO())
	if err != nil {
		fmt.Println(err)
	}

	device, err := object.EthernetCardTypes().CreateEthernetCard("vmxnet3", backing)
	if err != nil {
		fmt.Println(err)
	}

	devices = append(devices, device)

	deviceChange, err := devices.ConfigSpec(types.VirtualDeviceConfigSpecOperationAdd)
	if err != nil {
		fmt.Println(err)
	}

	spec.DeviceChange = deviceChange

	var datastorez *object.Datastore

	datastorez = ds

	dc, err := f.DatacenterOrDefault(context.TODO(), v.datacenter)
	if err != nil {
		fmt.Println(err)
		return err
	}

	folders, err := dc.Folders(context.TODO())
	if err != nil {
		fmt.Println(err)
	}

	spec.Files = &types.VirtualMachineFileInfo{
		VmPathName: fmt.Sprintf("[%s]", datastorez.Name()),
	}

	folder := folders.VmFolder

	pool, err := f.ResourcePoolOrDefault(context.TODO(), v.resourcePool)
	if err != nil {
		fmt.Println(err)
		fmt.Println("Did you set the correct Resource Pool? https://nanovms.gitbook.io/ops/vsphere#create-instance ")
		os.Exit(1)
	}

	task, err := folder.CreateVM(context.TODO(), *spec, pool, nil)
	if err != nil {
		fmt.Println(err)
		return err
	}

	info, err := task.WaitForResult(context.TODO(), nil)
	if err != nil {
		fmt.Printf("%+v", info)
		fmt.Printf("%+v", info.Reason)
		fmt.Println(err)
		return err
	}

	vm := object.NewVirtualMachine(v.client, info.Result.(types.ManagedObjectReference))

	devices, err = vm.Device(context.TODO())
	if err != nil {
		return err
	}

	// add serial for logs
	serial, err := devices.CreateSerialPort()
	if err != nil {
		fmt.Println(err)
	}

	err = vm.AddDevice(context.TODO(), serial)
	if err != nil {
		return err
	}

	devices, err = vm.Device(context.TODO())
	if err != nil {
		return err
	}

	d, err := devices.FindSerialPort("")
	if err != nil {
		return err
	}

	devices = devices.SelectByType(d)

	var mvm mo.VirtualMachine
	err = vm.Properties(context.TODO(), vm.Reference(), []string{"config.files.logDirectory"}, &mvm)
	if err != nil {
		return err
	}

	uri := path.Join(mvm.Config.Files.LogDirectory, "console.log")

	err = vm.EditDevice(context.TODO(), devices.ConnectSerialPort(d, uri, false, ""))
	if err != nil {
		fmt.Println(err)
	}

	return nil
}

// ListInstances lists instances on VSphere.
// It essentially does:
// govc ls /ha-datacenter/vm
func (v *Vsphere) ListInstances(ctx *Context) error {

	m := view.NewManager(v.client)

	cv, err := m.CreateContainerView(context.TODO(), v.client.ServiceContent.RootFolder, []string{"VirtualMachine"}, true)
	if err != nil {
		return err
	}

	defer cv.Destroy(context.TODO())

	var vms []mo.VirtualMachine
	err = cv.Retrieve(context.TODO(), []string{"VirtualMachine"}, []string{"summary"}, &vms)
	if err != nil {
		return err
	}

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"Name", "IP", "Status", "Created"})
	table.SetHeaderColor(
		tablewriter.Colors{tablewriter.Bold, tablewriter.FgCyanColor},
		tablewriter.Colors{tablewriter.Bold, tablewriter.FgCyanColor},
		tablewriter.Colors{tablewriter.Bold, tablewriter.FgCyanColor},
		tablewriter.Colors{tablewriter.Bold, tablewriter.FgCyanColor})
	table.SetRowLine(true)

	for _, vm := range vms {
		var row []string
		row = append(row, vm.Summary.Config.Name)

		ps := string(vm.Summary.Runtime.PowerState)
		if ps == "poweredOn" {
			ip := v.ipFor(vm.Summary.Config.Name)
			row = append(row, ip)
		} else {
			row = append(row, "")
		}

		row = append(row, string(vm.Summary.Runtime.PowerState))
		row = append(row, fmt.Sprintf("%s", vm.Summary.Runtime.BootTime))
		table.Append(row)
	}

	table.Render()

	return nil
}

// govc vm.ip -esxcli -wait 5s dtest
// waits for up to 1hr!?? wtf
//
// if we get empty string set the following && try again
//  govc host.esxcli system settings advanced set -o /Net/GuestIPHack -i
//  1
func (v *Vsphere) ipFor(instancename string) string {

	f := find.NewFinder(v.client, true)

	dc, err := f.DatacenterOrDefault(context.TODO(), v.datacenter)
	if err != nil {
		fmt.Println(err)
	}

	f.SetDatacenter(dc)

	vm, err := f.VirtualMachine(context.TODO(), instancename)
	if err != nil {
		if _, ok := err.(*find.NotFoundError); ok {
			fmt.Println("can't find vm " + instancename)
		}
		fmt.Println(err)
	}

	var get func(*object.VirtualMachine) (string, error) = func(vm *object.VirtualMachine) (string, error) {

		guest := esxcli.NewGuestInfo(v.client)

		ticker := time.NewTicker(time.Millisecond * 500)
		defer ticker.Stop()

		icnt := 0

		for {
			select {
			case <-ticker.C:

				if icnt > 3 {
					v.setGuestIPHack()
				}

				ip, err := guest.IpAddress(vm)
				if err != nil {
					fmt.Println(err)
					return "", err
				}

				if ip != "0.0.0.0" {
					return ip, nil
				}

				icnt++

			}
		}
	}

	ip, err := get(vm)
	if err != nil {
		fmt.Println(err)
	}

	return ip
}

func (v *Vsphere) findHostPath() string {
	f := find.NewFinder(v.client, true)
	dc, err := f.DatacenterOrDefault(context.TODO(), v.datacenter)
	if err != nil {
		fmt.Println(err)
	}

	f.SetDatacenter(dc)

	host, err := f.DefaultHostSystem(context.TODO())
	if err != nil {
		fmt.Println(err)
	}

	return host.InventoryPath
}

func (v *Vsphere) runCLI(args []string) (*esxcli.Response, error) {
	f := find.NewFinder(v.client, true)

	hostPath := v.findHostPath()
	host, err := f.HostSystemOrDefault(context.TODO(), hostPath)
	if err != nil {
		fmt.Println(err)
	}

	e, err := esxcli.NewExecutor(v.client, host)
	if err != nil {
		fmt.Println(err)
	}

	return e.Run(args)
}

func (v *Vsphere) iphackEnabled() bool {
	args := []string{"system", "settings", "advanced", "list", "-o", "/Net/GuestIPHack"}
	res, err := v.runCLI(args)
	if err != nil {
		fmt.Println(err)
	}

	for _, val := range res.Values {
		if ival, ok := val["IntValue"]; ok {
			if ival[0] == "1" {
				return true
			}
		}
	}

	return false
}

func (v *Vsphere) setGuestIPHack() {
	if v.iphackEnabled() {
		fmt.Println("ip hack enabled")
	} else {
		fmt.Println("setting ip hack")

		args := []string{"system", "settings", "advanced", "set", "-o", "/Net/GuestIPHack", "-i", "1"}

		res, err := v.runCLI(args)
		if err != nil {
			fmt.Println(err)
		}

		debug := false // FIXME: should have a debug log throughout OPS
		if debug {
			for _, val := range res.Values {
				fmt.Println(val)
			}

		}

	}

	fmt.Println("IP hack has been enabled for all new ARP requests, however, for existing hosts the easiest way to trigger that is to simply reboot the vm.")
	os.Exit(0)
}

// DeleteInstance deletes instance from VSphere
func (v *Vsphere) DeleteInstance(ctx *Context, instancename string) error {
	f := find.NewFinder(v.client, true)

	dc, err := f.DatacenterOrDefault(context.TODO(), v.datacenter)
	if err != nil {
		fmt.Println(err)
		return err
	}

	f.SetDatacenter(dc)

	vms, err := f.VirtualMachineList(context.TODO(), instancename)
	if err != nil {
		if _, ok := err.(*find.NotFoundError); ok {
			fmt.Println("can't find vm " + instancename)
		}
		fmt.Println(err)
	}

	vm := vms[0]

	task, err := vm.PowerOff(context.TODO())
	if err != nil {
		fmt.Println(err)
	}

	// Ignore error since the VM may already been in powered off
	// state.
	// vm.Destroy will fail if the VM is still powered on.
	_ = task.Wait(context.TODO())

	task, err = vm.Destroy(context.TODO())
	if err != nil {
		return err
	}

	err = task.Wait(context.TODO())
	if err != nil {
		return err
	}

	return nil
}

// StartInstance starts an instance in VSphere.
// It is the equivalent of:
// govc vm.power -on=true <instance_name>
func (v *Vsphere) StartInstance(ctx *Context, instancename string) error {
	f := find.NewFinder(v.client, true)

	dc, err := f.DatacenterOrDefault(context.TODO(), v.datacenter)
	if err != nil {
		fmt.Println(err)
		return err
	}

	f.SetDatacenter(dc)

	vms, err := f.VirtualMachineList(context.TODO(), instancename)
	if err != nil {
		if _, ok := err.(*find.NotFoundError); ok {
			fmt.Println("can't find vm " + instancename)
		}
		fmt.Println(err)
	}

	task, err := vms[0].PowerOn(context.TODO())
	if err != nil {
		fmt.Println(err)
	}

	_, err = task.WaitForResult(context.TODO(), nil)
	return err
}

// StopInstance stops an instance from VSphere
// It is the equivalent of:
// govc vm.power -on=false <instance_name>
func (v *Vsphere) StopInstance(ctx *Context, instancename string) error {
	f := find.NewFinder(v.client, true)

	dc, err := f.DatacenterOrDefault(context.TODO(), v.datacenter)
	if err != nil {
		fmt.Println(err)
		return err
	}

	f.SetDatacenter(dc)

	vms, err := f.VirtualMachineList(context.TODO(), instancename)
	if err != nil {
		if _, ok := err.(*find.NotFoundError); ok {
			fmt.Println("can't find vm " + instancename)
		}
		fmt.Println(err)
	}

	task, err := vms[0].PowerOff(context.TODO())
	if err != nil {
		fmt.Println(err)
	}

	_, err = task.WaitForResult(context.TODO(), nil)
	return err
}

// GetInstanceLogs gets instance related logs.
// govc datastore.tail -n 100 gtest/serial.out
// logs don't appear until you spin up the instance.
func (v *Vsphere) GetInstanceLogs(ctx *Context, instancename string, watch bool) error {

	f := find.NewFinder(v.client, true)
	ds, err := f.DatastoreOrDefault(context.TODO(), v.datastore)
	if err != nil {
		fmt.Println(err)
		return err
	}

	_, err = f.DefaultHostSystem(context.TODO())
	if err != nil {
		fmt.Println(err)
	}

	serialFile := instancename + "/console.log"

	file, err := ds.Open(context.TODO(), serialFile)
	if err != nil {
		return err
	}

	var reader io.ReadCloser = file

	err = file.Tail(100)
	if err != nil {
		return err
	}

	_, err = io.Copy(os.Stdout, reader)

	_ = reader.Close()

	return nil
}

// Todo - make me shared
func (v *Vsphere) customizeImage(ctx *Context) (string, error) {
	imagePath := ctx.config.RunConfig.Imagename
	return imagePath, nil
}

func (v *Vsphere) getCredentials() (*url.URL, error) {
	var tempURL string
	gu := os.Getenv("GOVC_URL")
	if gu == "" {
		return nil, fmt.Errorf("At the very least GOVC_URL should be set to https://host:port")
	}
	// warn if HTTP?
	if !strings.Contains(gu, "http") {
		tempURL = "https://" + gu
	} else {
		tempURL = gu
	}
	u, err := url.Parse(tempURL + "/sdk")
	if err != nil {
		return nil, err
	}

	// if credential is found and not empty string, return immediately
	un := u.User.Username()
	up, ok := u.User.Password()
	if un != "" && up != "" && ok {
		return u, nil
	}

	if un == "" {
		un = os.Getenv("GOVC_USERNAME")
	}
	if un == "" {
		return nil, fmt.Errorf("Incomplete credentials, set either via <GOVC_URL> with https://username:password@host:port or <GOVC_USERNAME and GOVC_PASSWORD>")
	}
	var pw string
	if ok {
		pw = up
	} else {
		pw = os.Getenv("GOVC_PASSWORD")
	}
	if pw == "" {
		return nil, fmt.Errorf("Incomplete credentials, set either via <GOVC_URL> with https://username:password@host:port or <GOVC_USERNAME and GOVC_PASSWORD>")
	}

	tempURL = fmt.Sprintf("%s://%s:%s@%s", u.Scheme, un, pw, u.Host)
	u, err = url.Parse(tempURL + "/sdk")
	return u, err
}
