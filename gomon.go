// gomon is a tool that watches a go program and automatically restart the
// application when a file change is detected.
package main

import (
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
)

var (
	version = "0.0.1"
	process *os.Process
)

var usage = `Usage: gomon <file|directory> <args>
Options:
  file - The filepath to watch for changes
  directory - The Project Directory To Watch For Changes
  args - The Arguments For The File / Project To Be Watched For Changes

  If No Option Is Received, It Will Treat The Current Directory As A Project Root And Try To Run It.
`

func main() {

	var (
		check fs.FileInfo
		err   error
		path  string
		args  []string
		cmd   string
	)

	helptext := flag.Bool("h", false, "Help Text")
	flag.Parse()

	if *helptext {
		helpText()
	}

	if len(os.Args) < 2 {
		path = "." //If There's No Arguments Passed, Assume User Wants To Run In Directory Mode On The Current Directory
	} else {
		path = os.Args[1] //Get The File Or Directory To Be (Watched | Run)
	}

	if len(os.Args) > 2 {
		args = os.Args[2:] //If More Than 1 Argument Is Passed, They're Positional Arguments For The (File | Project) To Be (Watched | Run)
	}

	check, err = os.Stat(path) //Get Details Of The (File | Directory) To Be (Watched | Run)
	if err != nil {
		log.Fatal(err)
	}

	//Initialize A New Fsnotify Watcher
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}
	defer watcher.Close()

	waiting := false
	//This Timer Serves As A Short Delay For Making Sure More Than 1 Write Doesn't Trigger Restarting The Program (More Than Once) Quickly
	//We Only Wanna Act On One Write Per Saved Change Change, Not Multiple Writes On One Saved Change
	timer := time.NewTimer(1000 * time.Millisecond)

	//Capture Ctrl-C (Interrupts) Or SIGTERM Signals For Graceful Shutdown (i.e Closing The Watcher And Making Sure The Last Spawned Process Is Killed Before Exiting)
	sigs := make(chan os.Signal, 1)

	//Void Channel To Make The Main Method Block Until A Signal Is Received
	echan := make(chan struct{})

	//Adds The Types Of Signal To Be Notified Of. (Currently SIGINT And SIGTERM).
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	//Files To Be Skipped | Not Watched For Changes (Will Make It User Specifiable Once I Figure Out A Clean Way)
	filesToSkip := []string{"vendor"}

	go func() {
		for {
			select {
			case e := <-watcher.Events:
				if e.Op.String() == "WRITE" || e.Op.String() == "WRITE|CHMOD" {
					if waiting {
						//This Should Only Execute If More Than One Writes Have Been Received On One Saved Change
						if !timer.Stop() {
							<-timer.C //Explicitly Drain The Timer Channel Before Resetting It (As Stated In The Documentation)
						}
						timer.Reset(500 * time.Millisecond) //Reset The Timer So We Have Enough Time To Catch Another Write Event
						continue
					}
					fmt.Println("\n==============\n[gomon] detected change")
					fmt.Println("[gomon] waiting for 500ms to verify file closure")
					//sets waiting to true so we can wait subsequent write events before restarting the program
					waiting = true
					timer.Reset(500 * time.Millisecond)
				}

			//This Channel Gets Written To Once The Timer Set Above Expires
			case <-timer.C:
				if waiting {
					fmt.Println("[gomon] no further change detected, restarting...")
					killPid(process)
					go runCmd(cmd)
					waiting = false
				}

			//If The Fsnotify Watcher Reports Any Errors, Print It Out.
			case err := <-watcher.Errors:
				log.Fatal(err)

			//Catch The Signal Sent To The Program
			case sig := <-sigs:
				fmt.Println()
				fmt.Printf("Signal Received: %v\n", sig)
				close(echan)
			}
		}
	}()

	/* This Is An Infinte Loop That Runs Along The Program, And Listens For A Command ["rst"] From STDIN
	If ["rst"] Is Received Then The Program Is Restarted, Albeit Manually
	*/
	go func() {
		var input string
		for {
			fmt.Scanln(&input)
			if input == "rst" {
				fmt.Println("[gomon] manual restart requested, restarting...")
				killPid(process)
				go runCmd(cmd)
			}
		}
	}()

	/* Remember The Check Variable That We Created Above?
	Here, It Is Used To Ascertain Whether A File Or A Directory Has Been Passed To gomon
	*/
	if check.IsDir() {
		fmt.Println("[gomon] Directory detected")
		//If The Directory Given Is Not The Current Directory, Change gomon's Working Directory To The Directory Specified
		//Then Change The 'path' Variable To '.' (This Is So We Can Do `go run .`)
		if path != "." {
			err := os.Chdir(path)
			if err != nil {
				log.Fatal(err)
			}
			path = "."
		}

		//Check If A go.mod File In The Directory Specified, If There Isn't, Exit
		if _, err := os.Stat(path + "/go.mod"); err != nil {
			log.Fatal("No go.mod File Found In Directory, Exiting")
		}

		/* Fsnotify Doesn't Provide Recursive Addition Of Files In SubDirectories
		So I Had To Get My Hands Dirty And Do It Myself :D
		*/
		filepath.Walk(path, func(docpath string, fileinfo os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			//If The Current Element Selected Is A Directory, Check If It Is Part Of The Directories We're To Skip Checks For
			if fileinfo.IsDir() {
				if contains(filesToSkip, fileinfo.Name()) {
					fmt.Printf("[gomon] Skipping Dir %v\n", fileinfo.Name())
					return filepath.SkipDir
				}

			} else {
				//Check If The Current File Is A Go File And ISN'T A Test File
				if filepath.Ext(fileinfo.Name()) == ".go" && !strings.Contains(fileinfo.Name(), "_test") {
					info(docpath)
					err = watcher.Add(docpath)
				}
			}

			return err
		})

		//Set The Command To Be Run To The Current Path.
		//Literally Translates To `cmd = "."`
		cmd = path
		go runCmd(cmd)
	} else {

		//Here We Are In The File Department
		fmt.Println("[gomon] File detected")

		//If The File That Was Passed As An Argument To gomon Is Not A Go File (Bad User, Bad User), Exit.
		if filepath.Ext(path) != ".go" {
			fmt.Println("[gomon] Cannot Run Non Golang File")
			return
		}

		info(path)

		if err := watcher.Add(path); err != nil {
			log.Fatal(err)
		}

		/*Now This Looks Inefficient Too, Why Am I Joining Here? Hold My TypeWriter
		Now, The Possibility That The Go File To Be Run Might Also Take Arguments Was Also Considered,
		So What This Pieces Of Spaghetti Stretches Out To, Is:
		path = "count.go"
		args = {"--count", "1", "2", "3", "4"}
		cmd Becomes = "count.go --count 1 2 3 4"
		Which Is Then Passed To runCmd Like So:
		runCmd("count.go --count 1 2 3 4")
		*/
		cmd = fmt.Sprintf("%s %s", path, strings.Join(args, " "))
		go runCmd(cmd)
	}

	//	go func() { <-done }()
	<-echan

	//Kill Last Created Child Before Exiting
	killPid(process)
}

//Prints The Files Being Watched For Changes
func info(f string) {
	fmt.Println("[gomon] watching changes for", f)
}

/*
runCmd: Runs The Specified Command
I Know This Looks Cryptic And Hacky But What Happens Here Is:
Variable 'args' Gets Created As A String Slice With An Initial Element "run",
Then It Gets Appended With Values Gotten From Splitting The Argument To This Function Which Is 'file' With WhiteSpace As A Delimiter
Sounds Complex Huh?, Bear With Me:
If For Instance, Variable 'file' = "count.go --values 1 2 3 4", The Above Explanation Can Be Visualized As
x := strings.Split(file, " ") //x = {"count.go", "--values", "1", "2", "3", "4"}
args = append(args, x) //args = {"run", "count.go", "--values", "1", "2", "3", "4"}
So, Executing It With exec.Command Looks Like:
cmd := exec.Command("go", "run", "count.go", "--values", "1", "2", "3", "4")
I Hope I've Been Able To Demystify This Cryptic Looking Function
*/
func runCmd(file string) {
	fmt.Println("[gomon] exec: go run", file)
	args := []string{"run"}                      //Creates A Slice With An Initial Element "run"
	args = append(args, strings.Fields(file)...) //Appends The Slice With Values Gotten From Splitting The 'file' Argument
	cmd := exec.Command("go", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	log.Print(cmd.Start(), "\n==============\nProgram Output:")

	if cmd.Process != nil {
		process = cmd.Process //Assign Process With The Current Running Instance's Process Image So We Can Terminate It Later.
	}
}

//Displays Usage Text
func helpText() {
	fmt.Println(usage)
	os.Exit(0)
}

//Kills Specified Process
func killPid(process *os.Process) {
	fmt.Printf("[gomon] Killing previous process: %d\n", process.Pid)
	if process != nil {
		syscall.Kill(-process.Pid, syscall.SIGKILL)
	}
}

/* For Some Reason, The Developers Of Golang Were Like:
"No Mr. Pichai, Developers Utilizing Golang Won't Even Remotely Need A Contains Method Attached To The Slice Object"
"They'll Be Fine Mr. Pichai"
Well, Guess What Google, I Made My Own contains Method (scoffs)
*/
func contains(arr []string, elem string) bool {
	if len(elem) > 1 && strings.HasPrefix(elem, ".") {
		return true
	}

	for _, i := range arr {
		if elem == i {
			return true
		}
	}
	return false
}
