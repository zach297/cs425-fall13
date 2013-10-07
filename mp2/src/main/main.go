package main

import (
    "bytes"
    "flag"
    "leader"
    "log"
    "math/rand"
    "membertable"
    "net"
    "os"
    "time"
)

var listenAddress = flag.String("bind", ":7777", "the address for listening")
var leaderAddress = flag.String("leader", "", "the address of the leader machine; leave unset to make this process leader")
var seedAddress = flag.String("seed", "", "the address of some machine to grab the inital membertable from")
var machineName = flag.String("name", "", "the name of this machine")
var logFile = flag.String("logs", "machine.log", "the file name to store the log in")

func sendHeartbeatToAddress(addr string, t *membertable.Table) error {
    // Connect to the given member
    conn, err := net.Dial("udp", addr)
    if err != nil {
        return err
    }

    defer conn.Close()

    var buffer bytes.Buffer

    // Write out the hearbeat to a packet
    if err = t.WriteTo(&buffer); err != nil {
        return err
    }

    _, err = conn.Write(buffer.Bytes())
    return err
}

func sendHeartbeat(me *membertable.Member, t *membertable.Table) error {
    // Get a list of members we can send our hearbeat to
    memberList := t.ActiveMembers()

    // We are alone on this earth :(
    if len(memberList) == 0 || (len(memberList) == 1 && memberList[0].ID == me.ID) {
        log.Println("So allooone")
        return nil
    }

    // Choose a member at random and send their heartbeat
    var sendToMember *membertable.Member
    for sendToMember == nil || sendToMember.ID == me.ID {
        sendToMember = &memberList[rand.Int() % len(memberList)]
    }

    return sendHeartbeatToAddress(sendToMember.Address, t)
}

func sendHeartbeatProcess(me *membertable.Member, t *membertable.Table, fatalChan chan bool) {
    for {
        me.HeartbeatID++
        t.MergeMember(*me)
        err := sendHeartbeat(me, t)
        if err != nil {
            log.Println(err)
        }
        time.Sleep(100 * time.Millisecond)
    }
    fatalChan <- true
}


func listenHeartbeatProccess(t *membertable.Table, fatalChan chan bool) {
    udpAddr, err := net.ResolveUDPAddr("udp", *listenAddress)
    if err != nil {
        log.Fatal(err)
    }

    ln, err := net.ListenUDP("udp", udpAddr)
    if err != nil {
        log.Fatal(err)
    }

    buffer := make([]byte, 1 << 16)
    for {
        bytesRead, _, err := ln.ReadFromUDP(buffer)

        if err != nil {
            log.Println(err)
            continue
        }
        if err = t.Update(bytes.NewBuffer(buffer[0:bytesRead])); err != nil {
            log.Println(err)
        }
    }

    fatalChan <- true
}

func leaderProcess(fatalChan chan bool) {
    if err := leader.Run(); err != nil {
        log.Fatal(err)
    }
    fatalChan <- true
}

func getIP(hostname string) string {
    machineIP, err := net.LookupHost(hostname)
    if err != nil || len(machineIP) == 0 {
        return ""
    }

    var preferredIP net.IP
    for _, ipStr := range machineIP {
        ip := net.ParseIP(ipStr)
        // Prefer IPv4 addresses that come sooner in the list of LookupHost
        if preferredIP == nil || preferredIP.To4() == nil && ip.To4() != nil {
            preferredIP = ip
        }
    }

    return preferredIP.String()
}

// Choose a color for a given ID
func getColor(id membertable.ID) string {
    switch id % 6 {
        case 0: return "1;31";
        case 1: return "1;32";
        case 2: return "1;34";
        case 3: return "1;33";
        case 4: return "1;35";
        case 5: return "1;36";
    }
    return "0";
}

func main() {
    flag.Parse()

    fatalChan := make(chan bool)

    // Get the machines name
    hostname, _ := os.Hostname()

    // Get the address that this machine can be contacted from if none was given
    bindAddress, bindPort, err := net.SplitHostPort(*listenAddress)
    if bindAddress == "" {
        bindAddress = getIP(hostname)
    }


    var id membertable.ID

    if *leaderAddress == "" {
        // We are the LEADER! Take an ID and take our role as Master of IDs.
        if id, err = leader.IncrementIDFile(); err != nil {
            log.Fatal(err)
            return
        }
        go leaderProcess(fatalChan)
    } else {
        // Get an ID from the leader
        if id, err = leader.RequestID(*leaderAddress); err != nil {
            log.Fatal(err)
            return
        }
    }


    var t membertable.Table
    t.Init()

    // Add ourselves to the table
    me := membertable.Member{
        ID: id,
        Name: *machineName,
        Address: bindAddress + ":" + bindPort,
        HeartbeatID: 0,
    }

    // If no name was given, default to the host name
    if me.Name == "" {
        me.Name = hostname
    }

    // Configure the log file to be something nice
    log.SetPrefix("[\x1B[" + getColor(me.ID) + "m" + me.Name + "\x1B[0m] ")
    log.SetFlags(0)

    log.Println("Hostname :", hostname)
    log.Println("Name     :", me.Name)
    log.Println("IP       :", bindAddress)
    log.Println("Address  :", me.Address)
    log.Println("ID       :", me.ID)

    t.JoinMember(&me)

    if *seedAddress != "" {
        log.Printf("sending heartbeat to seed member")
        if err = sendHeartbeatToAddress(*seedAddress, &t); err != nil {
            log.Fatal(err)
        }
    }

    go sendHeartbeatProcess(&me, &t, fatalChan)
    go listenHeartbeatProccess(&t, fatalChan)

    <-fatalChan
}