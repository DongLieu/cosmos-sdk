package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	dbm "github.com/cosmos/cosmos-db"
	"github.com/spf13/cast"
	"github.com/spf13/cobra"

	corestore "cosmossdk.io/core/store"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/cosmos/cosmos-sdk/version"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
)

const (
	flagTraceStore       = "trace-store"
	flagHeight           = "height"
	flagForZeroHeight    = "for-zero-height"
	flagJailAllowedAddrs = "jail-allowed-addrs"
	flagModulesToExport  = "modules-to-export"
)

// ExportCmd dumps app state to JSON.
func ExportCmd(appExporter servertypes.AppExporter) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export state to JSON",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			config := client.GetConfigFromCmd(cmd)
			viper := client.GetViperFromCmd(cmd)
			logger := client.GetLoggerFromCmd(cmd)

			if _, err := os.Stat(config.GenesisFile()); os.IsNotExist(err) {
				return err
			}

			db, err := openDB(config.RootDir, getAppDBBackend(viper))
			if err != nil {
				return err
			}

			if appExporter == nil {
				if _, err := fmt.Fprintln(cmd.ErrOrStderr(), "WARNING: App exporter not defined. Returning genesis file."); err != nil {
					return err
				}

				// Open file in read-only mode so we can copy it to stdout.
				// It is possible that the genesis file is large,
				// so we don't need to read it all into memory
				// before we stream it out.
				f, err := os.OpenFile(config.GenesisFile(), os.O_RDONLY, 0)
				if err != nil {
					return err
				}
				defer f.Close()

				if _, err := io.Copy(cmd.OutOrStdout(), f); err != nil {
					return err
				}

				return nil
			}

			height, _ := cmd.Flags().GetInt64(flagHeight)
			forZeroHeight, _ := cmd.Flags().GetBool(flagForZeroHeight)
			jailAllowedAddrs, _ := cmd.Flags().GetStringSlice(flagJailAllowedAddrs)
			modulesToExport, _ := cmd.Flags().GetStringSlice(flagModulesToExport)
			outputDocument, _ := cmd.Flags().GetString(flags.FlagOutputDocument)

			exported, err := appExporter(logger, db, nil, height, forZeroHeight, jailAllowedAddrs, viper, modulesToExport)
			if err != nil {
				return fmt.Errorf("error exporting state: %w", err)
			}

			appGenesis, err := genutiltypes.AppGenesisFromFile(config.GenesisFile())
			if err != nil {
				return err
			}

			// set current binary version
			appGenesis.AppName = version.AppName
			appGenesis.AppVersion = version.Version

			appGenesis.AppState = exported.AppState
			appGenesis.InitialHeight = exported.Height
			appGenesis.Consensus = genutiltypes.NewConsensusGenesis(exported.ConsensusParams, exported.Validators)

			out, err := json.Marshal(appGenesis)
			if err != nil {
				return err
			}

			if outputDocument == "" {
				// Copy the entire genesis file to stdout.
				_, err := io.Copy(cmd.OutOrStdout(), bytes.NewReader(out))
				return err
			}

			if err = appGenesis.SaveAs(outputDocument); err != nil {
				return err
			}

			return nil
		},
	}

	cmd.Flags().Int64(flagHeight, -1, "Export state from a particular height (-1 means latest height)")
	cmd.Flags().Bool(flagForZeroHeight, false, "Export state to start at height zero (perform preproccessing)")
	cmd.Flags().StringSlice(flagJailAllowedAddrs, []string{}, "Comma-separated list of operator addresses of jailed validators to unjail")
	cmd.Flags().StringSlice(flagModulesToExport, []string{}, "Comma-separated list of modules to export. If empty, will export all modules")
	cmd.Flags().String(flags.FlagOutputDocument, "", "Exported state is written to the given file instead of STDOUT")

	return cmd
}

// OpenDB opens the application database using the appropriate driver.
func openDB(rootDir string, backendType dbm.BackendType) (corestore.KVStoreWithBatch, error) {
	dataDir := filepath.Join(rootDir, "data")
	return dbm.NewDB("application", backendType, dataDir)
}

// GetAppDBBackend gets the backend type to use for the application DBs.
func getAppDBBackend(opts servertypes.AppOptions) dbm.BackendType {
	rv := cast.ToString(opts.Get("app-db-backend"))
	if len(rv) == 0 {
		rv = cast.ToString(opts.Get("db_backend"))
	}

	// Cosmos SDK has migrated to cosmos-db which does not support all the backends which tm-db supported
	if rv == "cleveldb" || rv == "badgerdb" || rv == "boltdb" {
		panic(fmt.Sprintf("invalid app-db-backend %q, use %q, %q, %q instead", rv, dbm.GoLevelDBBackend, dbm.PebbleDBBackend, dbm.RocksDBBackend))
	}

	if len(rv) != 0 {
		return dbm.BackendType(rv)
	}

	return dbm.GoLevelDBBackend
}
