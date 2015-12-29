package _tests_test

import (
	"bytes"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"os/user"
	"path"
	"testing"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/onsi/gomega/gexec"
)

const (
	deisWorkflowServiceHost = "DEIS_WORKFLOW_SERVICE_HOST"
	deisWorkflowServicePort = "DEIS_WORKFLOW_SERVICE_PORT"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

func getRandAppName() string {
	return fmt.Sprintf("test-%d", rand.Intn(1000))
}

func TestTests(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Tests Suite")
}

var (
	randSuffix        = rand.Intn(1000)
	testAdminUser     = fmt.Sprintf("test-admin-%d", randSuffix)
	testAdminPassword = "asdf1234"
	testAdminEmail    = fmt.Sprintf("test-admin-%d@deis.io", randSuffix)
	testUser          = fmt.Sprintf("test-%d", randSuffix)
	testPassword      = "asdf1234"
	testEmail         = fmt.Sprintf("test-%d@deis.io", randSuffix)
	url               = getController()
)

var _ = BeforeSuite(func() {
	// use the "deis" executable in the search $PATH
	_, err := exec.LookPath("deis")
	Expect(err).NotTo(HaveOccurred())

	// register the test-admin user
	register(url, testAdminUser, testAdminPassword, testAdminEmail)
	// verify this user is an admin by running a privileged command
	sess, err := start("deis users:list")
	Expect(err).To(BeNil())
	Eventually(sess).Should(gexec.Exit(0))

	// register the test user and add a key
	register(url, testUser, testPassword, testEmail)
	createKey("deis-test")
	sess, err = start("deis keys:add ~/.ssh/deis-test.pub")
	Expect(err).To(BeNil())
	Eventually(sess).Should(gexec.Exit(0))
	Eventually(sess).Should(gbytes.Say("Uploading deis-test.pub to deis... done"))
})

var _ = AfterSuite(func() {
	cancelUserSess, cancelUserErr := cancelSess(url, testUser, testPassword)
	cancelAdminSess, cancelAdminErr := cancelSess(url, testAdminUser, testAdminPassword)
	Expect(cancelUserErr).To(BeNil())
	Expect(cancelAdminErr).To(BeNil())
	cancelUserSess.Wait()
	cancelAdminSess.Wait()
	Expect(cancelUserSess.ExitCode()).To(BeZero())
	Expect(cancelAdminSess.ExitCode()).To(BeZero())
	Expect(cancelUserSess.Out.Contents()).To(ContainSubstring("Account cancelled"))
	Expect(cancelAdminSess.Out.Contents()).To(ContainSubstring("Account cancelled"))
})

func register(url, username, password, email string) {
	sess, err := start("deis register %s --username=%s --password=%s --email=%s", url, username, password, email)
	Expect(err).To(BeNil())
	Eventually(sess).Should(gbytes.Say("Registered %s", username))
	Eventually(sess).Should(gbytes.Say("Logged in as %s", username))
}

func cancelSess(url, user, pass string) (*gexec.Session, error) {
	lgSess, err := loginSess(url, user, pass)
	if err != nil {
		return nil, err
	}
	lgSess.Wait()
	cmd := exec.Command("deis", "auth:cancel", fmt.Sprintf("--username=%s", user), fmt.Sprintf("--password=%s", pass), "--yes")
	return gexec.Start(cmd, GinkgoWriter, GinkgoWriter)
}

func cancel(url, username, password string) {
	// log in to the account
	login(url, username, password)

	// cancel the account
	sess, err := start("deis auth:cancel --username=%s --password=%s --yes", username, password)
	Expect(err).To(BeNil())
	Eventually(sess).Should(gexec.Exit(0))
	Eventually(sess).Should(gbytes.Say("Account cancelled"))
}

func loginSess(url, user, pass string) (*gexec.Session, error) {
	cmd := exec.Command("deis", "login", url, fmt.Sprintf("--username=%s", user), fmt.Sprintf("--password=%s", pass))
	return gexec.Start(cmd, GinkgoWriter, GinkgoWriter)
}

func login(url, user, password string) {
	sess, err := start("deis login %s --username=%s --password=%s", url, user, password)
	Expect(err).To(BeNil())
	Eventually(sess).Should(gexec.Exit(0))
	Eventually(sess).Should(gbytes.Say("Logged in as %s", user))
}

func logout() {
	sess, err := start("deis auth:logout")
	Expect(err).To(BeNil())
	Eventually(sess).Should(gexec.Exit(0))
	Eventually(sess).Should(gbytes.Say("Logged out\n"))
}

// execute executes the command generated by fmt.Sprintf(cmdLine, args...) and returns its output as a cmdOut structure.
// this structure can then be matched upon using the SucceedWithOutput matcher below
func execute(cmdLine string, args ...interface{}) (string, error) {
	var stdout, stderr bytes.Buffer
	var cmd *exec.Cmd
	cmd = exec.Command("/bin/sh", "-c", fmt.Sprintf(cmdLine, args...))
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return stderr.String(), err
	}
	return stdout.String(), nil
}

func start(cmdLine string, args ...interface{}) (*gexec.Session, error) {
	cmdStr := fmt.Sprintf(cmdLine, args...)
	fmt.Println(cmdStr)
	cmd := exec.Command("/bin/sh", "-c", cmdStr)
	return gexec.Start(cmd, GinkgoWriter, GinkgoWriter)
}

func createKey(name string) {
	var home string
	if user, err := user.Current(); err != nil {
		home = "~"
	} else {
		home = user.HomeDir
	}
	path := path.Join(home, ".ssh", name)
	// create the key under ~/.ssh/<name> if it doesn't already exist
	if _, err := os.Stat(path); os.IsNotExist(err) {
		sess, err := start("ssh-keygen -q -t rsa -b 4096 -C %s -f %s -N ''", name, path)
		Expect(err).To(BeNil())
		Eventually(sess).Should(gexec.Exit(0))
	}
	// add the key to ssh-agent
	sess, err := start("eval $(ssh-agent) && ssh-add %s", path)
	Expect(err).To(BeNil())
	Eventually(sess).Should(gexec.Exit(0))
}

func getController() string {
	host := os.Getenv(deisWorkflowServiceHost)
	if host == "" {
		panicStr := fmt.Sprintf(`Set %s to the workflow controller hostname for tests, such as:

$ %s=deis.10.245.1.3.xip.io make test-integration`, deisWorkflowServiceHost, deisWorkflowServiceHost)
		panic(panicStr)
	}
	port := os.Getenv(deisWorkflowServicePort)
	switch port {
	case "443":
		return "https://" + host
	case "80", "":
		return "http://" + host
	default:
		return fmt.Sprintf("http://%s:%s", host, port)
	}
}