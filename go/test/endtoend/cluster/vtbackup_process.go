/*
Copyright 2019 The Vitess Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package cluster

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"strings"
	"syscall"
	"time"

	"vitess.io/vitess/go/vt/log"
)

// VtbackupProcess is a generic handle for a running Vtbackup.
// It can be spawned manually
type VtbackupProcess struct {
	VtProcess
	LogDir    string
	MysqlPort int
	Directory string

	BackupStorageImplementation string
	FileBackupStorageRoot       string

	Cell        string
	Keyspace    string
	Shard       string
	TabletAlias string
	Server      string

	ExtraArgs     []string
	initialBackup bool
	initDBfile    string

	proc *exec.Cmd
	exit chan error
}

// Setup starts vtbackup process with required arguements
func (vtbackup *VtbackupProcess) Setup() (err error) {
	vtbackup.proc = exec.Command(
		vtbackup.Binary,
		"--topo_implementation", vtbackup.TopoImplementation,
		"--topo_global_server_address", vtbackup.TopoGlobalAddress,
		"--topo_global_root", vtbackup.TopoGlobalRoot,
		"--log_dir", vtbackup.LogDir,

		//initDBfile is required to run vtbackup
		"--mysql_port", fmt.Sprintf("%d", vtbackup.MysqlPort),
		"--init_db_sql_file", vtbackup.initDBfile,
		"--init_keyspace", vtbackup.Keyspace,
		"--init_shard", vtbackup.Shard,

		//Backup Arguments are not optional
		"--backup_storage_implementation", vtbackup.BackupStorageImplementation,
		"--file_backup_storage_root", vtbackup.FileBackupStorageRoot,
	)

	if vtbackup.initialBackup {
		vtbackup.proc.Args = append(vtbackup.proc.Args, "--initial_backup")
	}
	if vtbackup.ExtraArgs != nil {
		vtbackup.proc.Args = append(vtbackup.proc.Args, vtbackup.ExtraArgs...)
	}

	vtbackup.proc.Stderr = os.Stderr
	vtbackup.proc.Stdout = os.Stdout

	vtbackup.proc.Env = append(vtbackup.proc.Env, os.Environ()...)
	vtbackup.proc.Env = append(vtbackup.proc.Env, DefaultVttestEnv)
	log.Infof("Running vtbackup with args: %v", strings.Join(vtbackup.proc.Args, " "))

	err = vtbackup.proc.Run()
	if err != nil {
		return
	}

	vtbackup.exit = make(chan error)
	go func() {
		if vtbackup.proc != nil {
			vtbackup.exit <- vtbackup.proc.Wait()
			close(vtbackup.exit)
		}
	}()

	return nil
}

// TearDown shutdowns the running vtbackup process
func (vtbackup *VtbackupProcess) TearDown() error {
	if vtbackup.proc == nil || vtbackup.exit == nil {
		return nil
	}

	// Attempt graceful shutdown with SIGTERM first
	vtbackup.proc.Process.Signal(syscall.SIGTERM)

	select {
	case err := <-vtbackup.exit:
		vtbackup.proc = nil
		return err

	case <-time.After(10 * time.Second):
		vtbackup.proc.Process.Kill()
		err := <-vtbackup.exit
		vtbackup.proc = nil
		return err
	}
}

// VtbackupProcessInstance returns a vtbackup handle
// configured with the given Config.
// The process must be manually started by calling Setup()
func VtbackupProcessInstance(tabletUID int, mysqlPort int, newInitDBFile string, keyspace string, shard string,
	cell string, hostname string, tmpDirectory string, topoPort int, initialBackup bool) *VtbackupProcess {
	base := VtProcessInstance("vtbackup", "vtbackup", topoPort, hostname)
	vtbackup := &VtbackupProcess{
		VtProcess:                   base,
		LogDir:                      tmpDirectory,
		Directory:                   os.Getenv("VTDATAROOT"),
		BackupStorageImplementation: "file",
		FileBackupStorageRoot:       path.Join(os.Getenv("VTDATAROOT"), "/backups"),
		TabletAlias:                 fmt.Sprintf("%s-%010d", cell, tabletUID),
		initDBfile:                  newInitDBFile,
		Keyspace:                    keyspace,
		Shard:                       shard,
		Cell:                        cell,
		MysqlPort:                   mysqlPort,
		initialBackup:               initialBackup,
	}
	return vtbackup
}
